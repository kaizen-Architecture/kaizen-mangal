package network

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/kaizen-architecture/kaizen-mangal/where"
	"github.com/spf13/viper"
	"golang.org/x/net/publicsuffix"
)

// PoliteRoundTripper enforces deterministic politeness (RFC 0001 Pillar B):
// 1. Per-host concurrency cap (network.domain_max_concurrency)
// 2. Cross-process per-domain minimum request spacing (network.domain_min_interval_ms)
// 3. Single Retry-After honor on 429/503 capped by network.retry_after_max
type PoliteRoundTripper struct {
	Base http.RoundTripper

	// Optional overrides for deterministic unit tests (0 falls back to Viper config).
	MinIntervalOverride    time.Duration
	MaxConcurrencyOverride int
	RetryAfterMaxOverride  time.Duration
	CacheDirOverride       string

	mu    sync.Mutex
	sems  map[string]chan struct{}
	semSz int
}

func (p *PoliteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := p.Base
	if base == nil {
		base = http.DefaultTransport
	}

	host := ""
	if req.URL != nil {
		host = strings.ToLower(req.URL.Hostname())
	}

	// 1. Per-host concurrency cap
	if host != "" {
		release, err := p.acquireHost(req.Context(), host)
		if err != nil {
			return nil, NormalizeError(err, host, 0)
		}
		defer release()
	}

	// 2. Cross-process per-domain spacing
	if host != "" {
		if interval := p.minInterval(); interval > 0 {
			domain := registrableDomain(host)
			if err := p.waitDomainSpacing(req.Context(), domain, interval); err != nil {
				return nil, NormalizeError(err, host, 0)
			}
		}
	}

	resp, err := base.RoundTrip(req)
	if err != nil {
		return nil, NormalizeError(err, host, 0)
	}

	// 3. Honor Retry-After once on 429/503 if within cap and request body is replayable
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		if delay, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			maxDelay := p.retryAfterMax()
			if delay > 0 && delay <= maxDelay && isReplayable(req) {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()

				if err := sleepContext(req.Context(), delay); err != nil {
					return nil, NormalizeError(err, host, 0)
				}

				retryReq := req.Clone(req.Context())
				if req.GetBody != nil {
					body, berr := req.GetBody()
					if berr != nil {
						return nil, NormalizeError(berr, host, 0)
					}
					retryReq.Body = body
				}
				return base.RoundTrip(retryReq)
			}
		}
	}

	return resp, nil
}

func (p *PoliteRoundTripper) acquireHost(ctx context.Context, host string) (func(), error) {
	limit := p.maxConcurrency()
	if limit <= 0 {
		return func() {}, nil
	}

	p.mu.Lock()
	if p.sems == nil || p.semSz != limit {
		p.sems = make(map[string]chan struct{})
		p.semSz = limit
	}
	sem, ok := p.sems[host]
	if !ok {
		sem = make(chan struct{}, limit)
		p.sems[host] = sem
	}
	p.mu.Unlock()

	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *PoliteRoundTripper) minInterval() time.Duration {
	if p.MinIntervalOverride > 0 {
		return p.MinIntervalOverride
	}
	ms := viper.GetInt(key.NetworkDomainMinIntervalMs)
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func (p *PoliteRoundTripper) maxConcurrency() int {
	if p.MaxConcurrencyOverride > 0 {
		return p.MaxConcurrencyOverride
	}
	if v := viper.GetInt(key.NetworkDomainMaxConcurrency); v > 0 {
		return v
	}
	return 8
}

func (p *PoliteRoundTripper) retryAfterMax() time.Duration {
	if p.RetryAfterMaxOverride > 0 {
		return p.RetryAfterMaxOverride
	}
	if sec := viper.GetInt(key.NetworkRetryAfterMax); sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return 30 * time.Second
}

func (p *PoliteRoundTripper) cacheDir() string {
	if p.CacheDirOverride != "" {
		_ = os.MkdirAll(p.CacheDirOverride, 0o755)
		return p.CacheDirOverride
	}
	return where.Cache()
}

// waitDomainSpacing coordinates per-domain spacing across processes via a wipe-tolerant,
// atomic JSON timestamp store in where.Cache() (RFC 0001 Pillar B & Section 6.F.5).
func (p *PoliteRoundTripper) waitDomainSpacing(ctx context.Context, domain string, interval time.Duration) error {
	storePath := filepath.Join(p.cacheDir(), "kaizen-mangal-ratelimit.json")
	lockPath := filepath.Join(p.cacheDir(), "kaizen-mangal-ratelimit.lock")

	var waitDur time.Duration
	unlock := acquireLockFile(lockPath, 2*time.Second)
	{
		state := readRateLimitState(storePath)
		now := time.Now()
		if lastUnixNano, ok := state[domain]; ok {
			nextAllowed := time.Unix(0, lastUnixNano).Add(interval)
			if nextAllowed.After(now) {
				waitDur = nextAllowed.Sub(now)
				state[domain] = nextAllowed.UnixNano()
			} else {
				state[domain] = now.UnixNano()
			}
		} else {
			state[domain] = now.UnixNano()
		}
		// Prune stale entries older than 10 minutes
		cutoff := now.Add(-10 * time.Minute).UnixNano()
		for k, ts := range state {
			if ts < cutoff {
				delete(state, k)
			}
		}
		_ = writeJSONAtomic(storePath, state)
	}
	unlock()

	if waitDur > 0 {
		return sleepContext(ctx, waitDur)
	}
	return nil
}

func readRateLimitState(path string) map[string]int64 {
	state := make(map[string]int64)
	data, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(data, &state)
	return state
}

// acquireLockFile uses O_CREATE|O_EXCL lockfiles with stale-lock recovery so it works
// portably on Linux, macOS, and Windows and tolerates mid-run cache wipes.
func acquireLockFile(lockPath string, maxWait time.Duration) func() {
	deadline := time.Now().Add(maxWait)
	for {
		_ = os.MkdirAll(filepath.Dir(lockPath), 0o755)
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() {
				_ = os.Remove(lockPath)
			}
		}
		if info, serr := os.Stat(lockPath); serr == nil && time.Since(info.ModTime()) > 10*time.Second {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return func() {}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".kaizen-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	_ = tmp.Chmod(0o600)
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	for attempt := 0; attempt < 5; attempt++ {
		if err := os.Rename(tmpPath, path); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	_ = os.Remove(tmpPath)
	return nil
}

func registrableDomain(host string) string {
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil || d == "" {
		return host
	}
	return d
}

func parseRetryAfter(val string, now time.Time) (time.Duration, bool) {
	val = strings.TrimSpace(val)
	if val == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(val); err == nil {
		if secs <= 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(val); err == nil {
		d := t.Sub(now)
		if d <= 0 {
			return 0, false
		}
		return d, true
	}
	return 0, false
}

func isReplayable(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
