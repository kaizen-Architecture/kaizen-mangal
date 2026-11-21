package network

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/spf13/viper"
)

// HeadlessFallbackSolver is the pluggable Tier 2 headless challenge solver hook,
// wired by internal/lualibs/headless in Phase 5.
var HeadlessFallbackSolver func(ctx context.Context, targetURL string, timeout time.Duration) (cookies []*http.Cookie, userAgent string, err error)

// ChallengeRoundTripper detects Cloudflare challenges on 403/503 responses,
// escalates through Tier 1 (FlareSolverr/Byparr) and Tier 2 (headless fallback),
// persists solved cookies + User-Agent into PersistentCookieJar, and enforces a per-domain circuit breaker.
type ChallengeRoundTripper struct {
	Base    http.RoundTripper
	Jar     *PersistentCookieJar
	Breaker *DomainCircuitBreaker

	// Optional test overrides
	SolverURLOverride      string
	SolverTimeoutOverride  time.Duration
	SolverFallbackOverride *bool
	SolverHTTPClient       *http.Client
}

// IsCloudflareChallenge returns true when resp+body match deterministic Cloudflare challenge signatures.
func IsCloudflareChallenge(resp *http.Response, body []byte) bool {
	if resp == nil {
		return false
	}
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusServiceUnavailable {
		return false
	}
	if strings.EqualFold(resp.Header.Get("cf-mitigated"), "challenge") {
		return true
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Server")), "cloudflare") {
		return false
	}
	sample := body
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	lower := strings.ToLower(string(sample))
	for _, marker := range []string{
		"just a moment",
		"cf_chl_opt",
		"challenges.cloudflare.com/turnstile",
		"_cf_chl_tk",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (c *ChallengeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := c.Base
	if base == nil {
		base = http.DefaultTransport
	}

	host := ""
	if req.URL != nil {
		host = strings.ToLower(req.URL.Hostname())
	}

	// Align User-Agent to any persisted solved User-Agent for this domain so cf_clearance validates.
	outReq := req
	if c.Jar != nil && host != "" {
		if solvedUA := c.Jar.DomainUserAgent(host); solvedUA != "" {
			outReq = req.Clone(req.Context())
			outReq.Header.Set("User-Agent", solvedUA)
		}
	}

	resp, err := base.RoundTrip(outReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusServiceUnavailable {
		return resp, nil
	}

	bodyBytes, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, NormalizeError(readErr, host, resp.StatusCode)
	}
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if !IsCloudflareChallenge(resp, bodyBytes) {
		return resp, nil
	}

	domain := registrableDomain(host)
	breaker := c.Breaker
	if breaker == nil {
		breaker = &DomainCircuitBreaker{}
	}

	// Circuit breaker check (Layer 1 deterministic fast-fail -> exit code 75)
	if !breaker.Allow(domain) {
		return nil, NewBreakerOpenError(domain)
	}

	cookies, ua, solveErr := c.solveChallenge(req.Context(), req.URL.String())
	if solveErr != nil {
		breaker.RecordFailure(domain)
		return nil, &StructuredError{
			Code:            "cf_challenge",
			Status:          resp.StatusCode,
			Domain:          domain,
			ContractVersion: ContractVersion,
			Message:         fmt.Sprintf("HTTP %d cloudflare challenge unsolved for %s: %v", resp.StatusCode, domain, solveErr),
			Cause:           solveErr,
		}
	}

	if c.Jar != nil {
		_ = c.Jar.SetSolvedSession(req.URL.String(), cookies, ua)
	}
	breaker.RecordSuccess(domain)

	if !isReplayable(req) {
		return resp, nil
	}

	retryReq := req.Clone(req.Context())
	if ua != "" {
		retryReq.Header.Set("User-Agent", ua)
	}
	if req.GetBody != nil {
		body, berr := req.GetBody()
		if berr != nil {
			return nil, NormalizeError(berr, host, 0)
		}
		retryReq.Body = body
	}

	// Clear any stale Cookie header so TLSClient attaches the newly persisted cookies from Jar.
	retryReq.Header.Del("Cookie")
	return base.RoundTrip(retryReq)
}

func (c *ChallengeRoundTripper) solverURL() string {
	if c.SolverURLOverride != "" {
		return c.SolverURLOverride
	}
	return strings.TrimSpace(viper.GetString(key.NetworkCloudflareSolverURL))
}

func (c *ChallengeRoundTripper) solverTimeout() time.Duration {
	if c.SolverTimeoutOverride > 0 {
		return c.SolverTimeoutOverride
	}
	if sec := viper.GetInt(key.NetworkCloudflareSolverTimeout); sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return 60 * time.Second
}

func (c *ChallengeRoundTripper) fallbackEnabled() bool {
	if c.SolverFallbackOverride != nil {
		return *c.SolverFallbackOverride
	}
	return viper.GetBool(key.NetworkCloudflareSolverFallback)
}

func (c *ChallengeRoundTripper) solveChallenge(ctx context.Context, targetURL string) ([]*http.Cookie, string, error) {
	timeout := c.solverTimeout()

	// Tier 1: External FlareSolverr / Byparr solver
	if endpoint := c.solverURL(); endpoint != "" {
		cookies, ua, err := c.solveViaFlareSolverr(ctx, endpoint, targetURL, timeout)
		if err == nil {
			return cookies, ua, nil
		}
		if !c.fallbackEnabled() {
			return nil, "", fmt.Errorf("external solver failed: %w", err)
		}
	}

	// Tier 2: Built-in headless fallback (OFF by default, Q3)
	if c.fallbackEnabled() {
		if HeadlessFallbackSolver == nil {
			return nil, "", errors.New("headless fallback is enabled (network.cloudflare_solver_fallback=true) but no local browser or headless.cdp_url solver is available")
		}
		return HeadlessFallbackSolver(ctx, targetURL, timeout)
	}

	return nil, "", errors.New("no cloudflare solver configured (set network.cloudflare_solver_url or enable network.cloudflare_solver_fallback)")
}

type flareSolverrRequest struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout"`
}

type flareSolverrResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		URL       string `json:"url"`
		Status    int    `json:"status"`
		UserAgent string `json:"userAgent"`
		Cookies   []struct {
			Name     string  `json:"name"`
			Value    string  `json:"value"`
			Domain   string  `json:"domain"`
			Path     string  `json:"path"`
			Expiry   float64 `json:"expiry"`
			Expires  float64 `json:"expires"`
			Secure   bool    `json:"secure"`
			HttpOnly bool    `json:"httpOnly"`
		} `json:"cookies"`
	} `json:"solution"`
}

func (c *ChallengeRoundTripper) solveViaFlareSolverr(ctx context.Context, endpoint, targetURL string, timeout time.Duration) ([]*http.Cookie, string, error) {
	payload, err := json.Marshal(flareSolverrRequest{
		Cmd:        "request.get",
		URL:        targetURL,
		MaxTimeout: int(timeout.Milliseconds()),
	})
	if err != nil {
		return nil, "", err
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := c.SolverHTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout + 5*time.Second}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("solver returned status %d", resp.StatusCode)
	}

	var parsed flareSolverrResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, "", fmt.Errorf("invalid solver JSON response: %w", err)
	}
	if !strings.EqualFold(parsed.Status, "ok") {
		return nil, "", fmt.Errorf("solver status=%q: %s", parsed.Status, parsed.Message)
	}

	cookies := make([]*http.Cookie, 0, len(parsed.Solution.Cookies))
	for _, sc := range parsed.Solution.Cookies {
		expSec := sc.Expiry
		if expSec == 0 {
			expSec = sc.Expires
		}
		var exp time.Time
		if expSec > 0 {
			exp = time.Unix(int64(expSec), 0)
		}
		cookies = append(cookies, &http.Cookie{
			Name:     sc.Name,
			Value:    sc.Value,
			Domain:   sc.Domain,
			Path:     sc.Path,
			Expires:  exp,
			Secure:   sc.Secure,
			HttpOnly: sc.HttpOnly,
		})
	}
	return cookies, parsed.Solution.UserAgent, nil
}
