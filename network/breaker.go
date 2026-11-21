package network

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/kaizen-architecture/kaizen-mangal/where"
	"github.com/spf13/viper"
)

const defaultBreakerFailureThreshold = 2

type domainBreakerEntry struct {
	ConsecutiveFailures int       `json:"consecutive_failures"`
	OpenUntil           time.Time `json:"open_until,omitempty"`
}

// DomainCircuitBreaker tracks per-domain challenge solve failures across processes
// in a wipe-tolerant, atomically written JSON file under where.Cache() (RFC 0001 Pillar C.3 & §10).
type DomainCircuitBreaker struct {
	CacheDirOverride string
	CooldownOverride time.Duration
	Threshold        int
}

func (b *DomainCircuitBreaker) storePath() string {
	dir := b.CacheDirOverride
	if dir == "" {
		dir = where.Cache()
	}
	return filepath.Join(dir, "kaizen-mangal-cf-breaker.json")
}

func (b *DomainCircuitBreaker) lockPath() string {
	dir := b.CacheDirOverride
	if dir == "" {
		dir = where.Cache()
	}
	return filepath.Join(dir, "kaizen-mangal-cf-breaker.lock")
}

func (b *DomainCircuitBreaker) cooldown() time.Duration {
	if b.CooldownOverride > 0 {
		return b.CooldownOverride
	}
	if sec := viper.GetInt(key.NetworkCloudflareBreakerCooldown); sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return 900 * time.Second
}

func (b *DomainCircuitBreaker) threshold() int {
	if b.Threshold > 0 {
		return b.Threshold
	}
	return defaultBreakerFailureThreshold
}

func (b *DomainCircuitBreaker) loadState() map[string]domainBreakerEntry {
	state := make(map[string]domainBreakerEntry)
	raw, err := os.ReadFile(b.storePath())
	if err != nil {
		return state
	}
	_ = json.Unmarshal(raw, &state)
	return state
}

// Allow returns true if the domain's circuit is Closed or HalfOpen (cooldown elapsed).
func (b *DomainCircuitBreaker) Allow(host string) bool {
	domain := registrableDomain(strings.ToLower(host))
	state := b.loadState()
	entry, ok := state[domain]
	if !ok {
		return true
	}
	if entry.OpenUntil.IsZero() {
		return true
	}
	return !time.Now().Before(entry.OpenUntil)
}

// RecordSuccess resets the consecutive failure counter and closes the circuit for domain.
func (b *DomainCircuitBreaker) RecordSuccess(host string) {
	domain := registrableDomain(strings.ToLower(host))
	unlock := acquireLockFile(b.lockPath(), 2*time.Second)
	defer unlock()

	state := b.loadState()
	delete(state, domain)
	_ = writeJSONAtomic(b.storePath(), state)
}

// RecordFailure increments the failure count and opens the circuit once threshold is reached.
func (b *DomainCircuitBreaker) RecordFailure(host string) {
	domain := registrableDomain(strings.ToLower(host))
	unlock := acquireLockFile(b.lockPath(), 2*time.Second)
	defer unlock()

	state := b.loadState()
	entry := state[domain]
	entry.ConsecutiveFailures++
	if entry.ConsecutiveFailures >= b.threshold() {
		entry.OpenUntil = time.Now().Add(b.cooldown())
	}
	state[domain] = entry
	_ = writeJSONAtomic(b.storePath(), state)
}
