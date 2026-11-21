package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestResolveProfilesAndHeaders(t *testing.T) {
	for _, name := range []string{"", "chrome_133", "chrome_131", "firefox_135", "firefox_133", "safari_16_0", "none"} {
		p, err := ResolveProfile(name)
		if err != nil {
			t.Fatalf("ResolveProfile(%q) unexpected error: %v", name, err)
		}
		if p.UserAgent == "" {
			t.Errorf("profile %q has empty UserAgent", name)
		}
		if strings.Contains(strings.ToLower(p.UserAgent), "mangal") {
			t.Errorf("impersonated profile %q UserAgent must not contain 'mangal': %q", name, p.UserAgent)
		}
	}

	if _, err := ResolveProfile("chrome_latest"); err == nil {
		t.Fatal("expected error for floating profile alias 'chrome_latest'")
	}
}

func TestTLSClientInterfaceAndFallback(t *testing.T) {
	for _, profileName := range []string{"chrome_133", "none"} {
		t.Run(profileName, func(t *testing.T) {
			var gotUA, gotReferer, gotAcceptLang string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUA = r.Header.Get("User-Agent")
				gotReferer = r.Header.Get("Referer")
				gotAcceptLang = r.Header.Get("Accept-Language")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "ok")
			}))
			defer srv.Close()

			var hc HTTPClient
			hc, err := NewClient(ClientOptions{
				Profile:           profileName,
				Timeout:           10 * time.Second,
				DisablePoliteness: true,
			})
			if err != nil {
				t.Fatalf("NewClient(%s) failed: %v", profileName, err)
			}

			req, err := http.NewRequest(http.MethodGet, srv.URL+"/test", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Referer", "https://example.org/chapter/1")

			resp, err := hc.Do(req)
			if err != nil {
				t.Fatalf("Do(%s) failed: %v", profileName, err)
			}
			_ = resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if gotReferer != "https://example.org/chapter/1" {
				t.Errorf("Referer = %q, want preserved caller Referer", gotReferer)
			}
			if gotUA == "" || strings.Contains(strings.ToLower(gotUA), "mangal") {
				t.Errorf("unexpected User-Agent = %q", gotUA)
			}
			if gotAcceptLang == "" {
				t.Errorf("expected profile Accept-Language header to be set")
			}
		})
	}
}

// kaizenRetryableSubstrings mirrors Kaizen/src/server/utils/mangal.ts:215-224 (RFC 0001 Q7).
var kaizenRetryableSubstrings = []string{
	"429",
	"rate limit",
	"500",
	"502",
	"503",
	"504",
	"522",
	"timeout",
	"connection refused",
}

func matchesKaizenRetryable(text string) (string, bool) {
	lower := strings.ToLower(text)
	for _, sub := range kaizenRetryableSubstrings {
		if strings.Contains(lower, sub) {
			return sub, true
		}
	}
	return "", false
}

func TestErrorContractAndKaizenSubstrings(t *testing.T) {
	// Retryable errors must match Kaizen's retryable substrings.
	retryableCases := []struct {
		name string
		err  error
		want string
	}{
		{"deadline_exceeded", NormalizeError(context.DeadlineExceeded, "example.org", 0), "timeout"},
		{"conn_refused", NormalizeError(syscall.ECONNREFUSED, "example.org", 0), "connection refused"},
		{"http_503", NormalizeError(errors.New("service unavailable"), "example.org", 503), "503"},
		{"http_429", NormalizeError(errors.New("too many requests"), "example.org", 429), "429"},
	}

	for _, tc := range retryableCases {
		t.Run(tc.name, func(t *testing.T) {
			combined := tc.err.Error() + "\n" + FormatErrorLine(tc.err)
			if !strings.Contains(strings.ToLower(combined), tc.want) {
				t.Fatalf("error %q does not contain expected retryable substring %q", combined, tc.want)
			}
			if ExitCodeFor(tc.err) != 1 {
				t.Fatalf("exit code = %d, want 1", ExitCodeFor(tc.err))
			}
			assertValidErrorLine(t, FormatErrorLine(tc.err))
		})
	}

	// Non-retryable errors must NEVER match any Kaizen retryable substring in either message or JSON line.
	nonRetryableCases := []struct {
		name     string
		err      error
		wantExit int
	}{
		{"breaker_open", NewBreakerOpenError("example.org"), ExitCodeBreakerOpen},
		{"cf_403", &StructuredError{
			Code:            "cf_challenge",
			Status:          403,
			Domain:          "example.org",
			ContractVersion: ContractVersion,
			Message:         "HTTP 403 cloudflare challenge unsolved for example.org",
		}, 1},
		{"integrity_failure", &StructuredError{
			Code:            "invalid_image",
			Domain:          "cdn.example.org",
			ContractVersion: ContractVersion,
			Message:         "invalid page #1 from cdn.example.org: unsupported magic bytes",
		}, 1},
	}

	for _, tc := range nonRetryableCases {
		t.Run(tc.name, func(t *testing.T) {
			line := FormatErrorLine(tc.err)
			assertValidErrorLine(t, line)
			if sub, hit := matchesKaizenRetryable(tc.err.Error()); hit {
				t.Errorf("non-retryable error message %q accidentally matched Kaizen retryable substring %q", tc.err.Error(), sub)
			}
			if sub, hit := matchesKaizenRetryable(line); hit {
				t.Errorf("non-retryable structured line %q accidentally matched Kaizen retryable substring %q", line, sub)
			}
			if got := ExitCodeFor(tc.err); got != tc.wantExit {
				t.Errorf("ExitCodeFor = %d, want %d", got, tc.wantExit)
			}
		})
	}
}

func assertValidErrorLine(t *testing.T, line string) {
	t.Helper()
	if !strings.HasPrefix(line, ErrorLinePrefix) {
		t.Fatalf("structured error line %q missing prefix %q", line, ErrorLinePrefix)
	}
	raw := strings.TrimPrefix(line, ErrorLinePrefix)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("structured error line invalid JSON: %v (%q)", err, raw)
	}
	if parsed["contract_version"] != float64(ContractVersion) {
		t.Fatalf("contract_version = %v, want %d", parsed["contract_version"], ContractVersion)
	}
	if parsed["code"] == "" || parsed["code"] == nil {
		t.Fatalf("structured error line missing 'code': %q", raw)
	}
}
