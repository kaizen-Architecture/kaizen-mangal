package network

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestPersistentCookieJarCrossProcessAndScoping(t *testing.T) {
	jarPath := filepath.Join(t.TempDir(), "kaizen-mangal-cookies.json")
	jarA := NewPersistentCookieJar(jarPath)
	jarB := NewPersistentCookieJar(jarPath)

	u, _ := url.Parse("https://sub.example.org/manga/1")
	if err := jarA.SetSolvedSession(u.String(), []*http.Cookie{
		{
			Name:    "cf_clearance",
			Value:   "token-abc",
			Domain:  ".example.org",
			Path:    "/",
			Expires: time.Now().Add(time.Hour),
			Secure:  true,
		},
		{
			Name:    "expired_cookie",
			Value:   "old",
			Domain:  ".example.org",
			Path:    "/",
			Expires: time.Now().Add(-time.Minute),
		},
	}, "CustomSolvedUA/133"); err != nil {
		t.Fatalf("SetSolvedSession failed: %v", err)
	}

	// Verify 0600 permissions on POSIX systems
	if runtime.GOOS != "windows" {
		info, err := os.Stat(jarPath)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("cookie jar permissions = %o, want 0600", perm)
		}
	}

	// Worker B must immediately see persisted cookie + UA (and NOT the expired cookie)
	got := jarB.Cookies(u)
	if len(got) != 1 || got[0].Name != "cf_clearance" || got[0].Value != "token-abc" {
		t.Fatalf("jarB.Cookies = %+v, want single cf_clearance=token-abc", got)
	}
	if ua := jarB.DomainUserAgent("cdn.example.org"); ua != "CustomSolvedUA/133" {
		t.Fatalf("jarB.DomainUserAgent(cdn.example.org) = %q, want CustomSolvedUA/133", ua)
	}

	// Secure cookie must not be sent over plain HTTP
	httpURL, _ := url.Parse("http://sub.example.org/manga/1")
	if c := jarB.Cookies(httpURL); len(c) != 0 {
		t.Fatalf("expected 0 cookies for Secure cookie over HTTP, got %+v", c)
	}

	// Different domain must not receive cookie
	otherURL, _ := url.Parse("https://other.net/")
	if c := jarB.Cookies(otherURL); len(c) != 0 {
		t.Fatalf("expected 0 cookies for other.net, got %+v", c)
	}

	// Mid-run cache wipe -> missing file equals empty state, no error
	_ = os.Remove(jarPath)
	_ = os.Remove(jarA.lockPath(jarPath))
	if c := jarB.Cookies(u); len(c) != 0 {
		t.Fatalf("expected empty cookies after cache wipe, got %+v", c)
	}
}

func TestPersistentCookieJarConcurrency(t *testing.T) {
	jarPath := filepath.Join(t.TempDir(), "kaizen-mangal-cookies.json")
	u, _ := url.Parse("https://example.org/ch/1")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			j := NewPersistentCookieJar(jarPath)
			j.SetCookies(u, []*http.Cookie{{
				Name:    fmt.Sprintf("k%d", i),
				Value:   fmt.Sprintf("v%d", i),
				Domain:  "example.org",
				Path:    "/",
				Expires: time.Now().Add(time.Hour),
			}})
			_ = j.Cookies(u)
		}()
	}
	wg.Wait()

	finalJar := NewPersistentCookieJar(jarPath)
	if len(finalJar.Cookies(u)) != 8 {
		t.Fatalf("expected 8 merged cookies after concurrent writes, got %d", len(finalJar.Cookies(u)))
	}
}
