package network

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoliteRoundTripperRetryAfterHonorAndCap(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if r.URL.Path == "/honor" && n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.URL.Path == "/exceeds-cap" {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &PoliteRoundTripper{
		Base:                  http.DefaultTransport,
		RetryAfterMaxOverride: 2 * time.Second,
		CacheDirOverride:      t.TempDir(),
	}

	// Retry-After=1s <= 2s cap -> sleeps ~1s and retries once -> 200 OK
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/honor", nil)
	start := time.Now()
	resp, err := rt.RoundTrip(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RoundTrip(/honor) failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 after Retry-After", resp.StatusCode)
	}
	if elapsed < 800*time.Millisecond {
		t.Errorf("expected Retry-After delay >= 800ms, got %v", elapsed)
	}

	// Retry-After=120s > 2s cap -> returns 429 immediately without sleeping
	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/exceeds-cap", nil)
	start2 := time.Now()
	resp2, err := rt.RoundTrip(req2)
	elapsed2 := time.Since(start2)
	if err != nil {
		t.Fatalf("RoundTrip(/exceeds-cap) failed: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 when Retry-After exceeds cap", resp2.StatusCode)
	}
	if elapsed2 > 500*time.Millisecond {
		t.Errorf("expected immediate return when Retry-After exceeds cap, took %v", elapsed2)
	}
}

func TestPoliteRoundTripperDomainSpacingAndWipeTolerance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	rt := &PoliteRoundTripper{
		Base:                http.DefaultTransport,
		MinIntervalOverride: 100 * time.Millisecond,
		CacheDirOverride:    cacheDir,
	}

	req1, _ := http.NewRequest(http.MethodGet, srv.URL+"/1", nil)
	resp1, err := rt.RoundTrip(req1)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp1.Body.Close()

	// Second request must wait ~100ms
	start := time.Now()
	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/2", nil)
	resp2, err := rt.RoundTrip(req2)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if elapsed := time.Since(start); elapsed < 70*time.Millisecond {
		t.Errorf("expected per-domain spacing >= 70ms, got %v", elapsed)
	}

	// Wipe all cache files mid-run -> next request must still succeed cleanly
	_ = os.Remove(filepath.Join(cacheDir, "kaizen-mangal-ratelimit.json"))
	_ = os.Remove(filepath.Join(cacheDir, "kaizen-mangal-ratelimit.lock"))

	req3, _ := http.NewRequest(http.MethodGet, srv.URL+"/3", nil)
	resp3, err := rt.RoundTrip(req3)
	if err != nil {
		t.Fatalf("RoundTrip after cache wipe failed: %v", err)
	}
	_ = resp3.Body.Close()
}

func TestPoliteRoundTripperPerHostConcurrencyCap(t *testing.T) {
	var inFlight atomic.Int32
	var maxObserved atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		for {
			prev := maxObserved.Load()
			if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		inFlight.Add(-1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &PoliteRoundTripper{
		Base:                   http.DefaultTransport,
		MaxConcurrencyOverride: 2,
		CacheDirOverride:       t.TempDir(),
	}

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/p", nil)
			resp, err := rt.RoundTrip(req)
			if err != nil {
				t.Errorf("RoundTrip failed: %v", err)
				return
			}
			_ = resp.Body.Close()
		}()
	}
	wg.Wait()

	if got := maxObserved.Load(); got > 2 {
		t.Fatalf("observed %d concurrent requests on host, cap is 2", got)
	}
}
