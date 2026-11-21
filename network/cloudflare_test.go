package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsCloudflareChallenge(t *testing.T) {
	r1 := &http.Response{StatusCode: 403, Header: http.Header{"Cf-Mitigated": {"challenge"}}}
	if !IsCloudflareChallenge(r1, nil) {
		t.Error("expected true for 403 + cf-mitigated: challenge")
	}

	r2 := &http.Response{StatusCode: 503, Header: http.Header{"Server": {"cloudflare"}}}
	if !IsCloudflareChallenge(r2, []byte("<html><title>Just a moment...</title></html>")) {
		t.Error("expected true for 503 + Server: cloudflare + Just a moment")
	}

	r3 := &http.Response{StatusCode: 403, Header: http.Header{"Server": {"nginx"}}}
	if IsCloudflareChallenge(r3, []byte("Forbidden")) {
		t.Error("expected false for regular nginx 403")
	}

	r4 := &http.Response{StatusCode: 200, Header: http.Header{"Server": {"cloudflare"}}}
	if IsCloudflareChallenge(r4, []byte("<html>Just a moment...</html>")) {
		t.Error("expected false for 200 OK")
	}
}

func TestChallengeRoundTripperFlareSolverrAndPersistence(t *testing.T) {
	const wantCookie = "solved-token-xyz"
	const wantUA = "SolvedChrome/133.0"

	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("cf_clearance")
		if err == nil && ck.Value == wantCookie && r.Header.Get("User-Agent") == wantUA {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "protected-content")
			return
		}
		w.Header().Set("Cf-Mitigated", "challenge")
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "<html><title>Just a moment...</title></html>")
	}))
	defer targetSrv.Close()

	var solverCalls atomic.Int32
	solverSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		solverCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"solution": map[string]any{
				"url":       targetSrv.URL,
				"status":    200,
				"userAgent": wantUA,
				"cookies": []map[string]any{
					{
						"name":   "cf_clearance",
						"value":  wantCookie,
						"domain": "127.0.0.1",
						"path":   "/",
						"expiry": float64(time.Now().Add(time.Hour).Unix()),
					},
				},
			},
		})
	}))
	defer solverSrv.Close()

	cacheDir := t.TempDir()
	jarPath := filepath.Join(cacheDir, "kaizen-mangal-cookies.json")
	jar := NewPersistentCookieJar(jarPath)
	breaker := &DomainCircuitBreaker{CacheDirOverride: cacheDir, CooldownOverride: time.Minute}

	tlsBase, err := NewTLSClient(ClientOptions{Profile: "none", Jar: jar})
	if err != nil {
		t.Fatal(err)
	}
	rt := &ChallengeRoundTripper{
		Base:                  tlsBase,
		Jar:                   jar,
		Breaker:               breaker,
		SolverURLOverride:     solverSrv.URL,
		SolverTimeoutOverride: 5 * time.Second,
	}

	// First request triggers challenge -> solves via FlareSolverr -> persists -> retries -> 200 OK
	req, _ := http.NewRequest(http.MethodGet, targetSrv.URL+"/page", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if solverCalls.Load() != 1 {
		t.Fatalf("solverCalls = %d, want 1", solverCalls.Load())
	}

	// Second process (fresh PersistentCookieJar) must reuse persisted cookie + UA with 0 solver calls
	jar2 := NewPersistentCookieJar(jarPath)
	tlsBase2, _ := NewTLSClient(ClientOptions{Profile: "none", Jar: jar2})
	rt2 := &ChallengeRoundTripper{
		Base:                  tlsBase2,
		Jar:                   jar2,
		Breaker:               breaker,
		SolverURLOverride:     solverSrv.URL,
		SolverTimeoutOverride: 5 * time.Second,
	}
	req2, _ := http.NewRequest(http.MethodGet, targetSrv.URL+"/page2", nil)
	resp2, err := rt2.RoundTrip(req2)
	if err != nil {
		t.Fatalf("second RoundTrip failed: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d, want 200", resp2.StatusCode)
	}
	if solverCalls.Load() != 1 {
		t.Fatalf("expected 0 additional solver calls on cached session, got total %d", solverCalls.Load())
	}
}

func TestChallengeCircuitBreakerOpenCooldownAndReset(t *testing.T) {
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "challenge")
	}))
	defer targetSrv.Close()

	var solverCalls atomic.Int32
	solverSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		solverCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer solverSrv.Close()

	cacheDir := t.TempDir()
	breaker := &DomainCircuitBreaker{
		CacheDirOverride: cacheDir,
		CooldownOverride: 150 * time.Millisecond,
		Threshold:        2,
	}
	rt := &ChallengeRoundTripper{
		Base:                  http.DefaultTransport,
		Jar:                   NewPersistentCookieJar(filepath.Join(cacheDir, "cookies.json")),
		Breaker:               breaker,
		SolverURLOverride:     solverSrv.URL,
		SolverTimeoutOverride: 2 * time.Second,
	}

	// 2 consecutive failures open the circuit
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, targetSrv.URL, nil)
		_, err := rt.RoundTrip(req)
		if err == nil {
			t.Fatalf("attempt %d: expected error", i+1)
		}
		if ExitCodeFor(err) != 1 {
			t.Fatalf("attempt %d: expected exit code 1, got %d", i+1, ExitCodeFor(err))
		}
	}
	if solverCalls.Load() != 2 {
		t.Fatalf("expected 2 solver calls, got %d", solverCalls.Load())
	}

	// 3rd request must fail fast with breaker-open (exit 75, 0 additional solver calls, 0 retryable substrings)
	req3, _ := http.NewRequest(http.MethodGet, targetSrv.URL, nil)
	_, err3 := rt.RoundTrip(req3)
	if err3 == nil {
		t.Fatal("expected breaker-open error on 3rd request")
	}
	if ExitCodeFor(err3) != ExitCodeBreakerOpen {
		t.Fatalf("exit code = %d, want %d", ExitCodeFor(err3), ExitCodeBreakerOpen)
	}
	if !strings.Contains(err3.Error(), BreakerOpenMarker) {
		t.Fatalf("error %q missing marker %q", err3.Error(), BreakerOpenMarker)
	}
	if sub, hit := matchesKaizenRetryable(err3.Error() + "\n" + FormatErrorLine(err3)); hit {
		t.Fatalf("breaker-open error matched retryable substring %q", sub)
	}
	if solverCalls.Load() != 2 {
		t.Fatalf("breaker open should not call solver; got %d calls", solverCalls.Load())
	}

	// Wait for cooldown -> HalfOpen allows retry
	time.Sleep(200 * time.Millisecond)
	if !breaker.Allow("127.0.0.1") {
		t.Fatal("expected breaker to allow after cooldown elapsed")
	}

	// Cache wipe of breaker file resets cleanly
	_ = os.Remove(breaker.storePath())
	_ = os.Remove(breaker.lockPath())
	if !breaker.Allow("127.0.0.1") {
		t.Fatal("expected breaker to allow after cache wipe")
	}
}

func TestTier2FallbackClearErrorWhenNoBrowserAvailable(t *testing.T) {
	prev := HeadlessFallbackSolver
	HeadlessFallbackSolver = nil
	t.Cleanup(func() { HeadlessFallbackSolver = prev })

	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer targetSrv.Close()

	fallback := true
	rt := &ChallengeRoundTripper{
		Base:                   http.DefaultTransport,
		Jar:                    NewPersistentCookieJar(filepath.Join(t.TempDir(), "cookies.json")),
		Breaker:                &DomainCircuitBreaker{CacheDirOverride: t.TempDir()},
		SolverFallbackOverride: &fallback,
	}

	req, _ := http.NewRequest(http.MethodGet, targetSrv.URL, nil)
	_, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error when Tier 2 fallback enabled with no browser")
	}
	if !strings.Contains(err.Error(), "headless.cdp_url") {
		t.Fatalf("expected clear error mentioning headless.cdp_url, got: %v", err)
	}
	var se *StructuredError
	if !errors.As(err, &se) || se.Code != "cf_challenge" {
		t.Fatalf("expected StructuredError code=cf_challenge, got %T %v", err, err)
	}
}
