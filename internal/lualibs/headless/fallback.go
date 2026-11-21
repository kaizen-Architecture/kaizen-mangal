package headless

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-rod/stealth"
	"github.com/kaizen-architecture/kaizen-mangal/network"
)

func init() {
	network.HeadlessFallbackSolver = SolveCloudflareChallenge
}

// SolveCloudflareChallenge is the Tier 2 headless challenge solver (RFC 0001 Pillar C.3 & Pillar D).
// It launches a stealth Chromium instance via ResolveBrowser(), navigates to targetURL,
// waits for cf_clearance + challenge DOM clearance within timeout, extracts cookies + UA,
// and always closes the browser cleanly.
func SolveCloudflareChallenge(ctx context.Context, targetURL string, timeout time.Duration) ([]*http.Cookie, string, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout()
	}

	res := ResolveBrowser()
	if res.Mode == "unavailable" {
		return nil, "", fmt.Errorf("headless fallback is enabled but no browser or headless.cdp_url is available: %s", res.Error)
	}

	mb, err := OpenBrowser(timeout)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = mb.Close() }()

	page, err := stealth.Page(mb.Browser)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open stealth page: %w", err)
	}
	defer func() { _ = page.Close() }()

	if err := page.Timeout(timeout).Navigate(targetURL); err != nil {
		return nil, "", fmt.Errorf("headless navigation failed: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		default:
		}

		rodCookies, cerr := mb.Browser.GetCookies()
		hasClearance := false
		if cerr == nil {
			for _, rc := range rodCookies {
				if rc.Name == "cf_clearance" && rc.Value != "" {
					hasClearance = true
					break
				}
			}
		}

		title := ""
		if info, ierr := page.Info(); ierr == nil && info != nil {
			title = strings.ToLower(info.Title)
		}
		stillChallenged := strings.Contains(title, "just a moment") || strings.Contains(title, "attention required")

		if hasClearance && !stillChallenged {
			ua := network.ActiveProfile().UserAgent
			if v, eerr := page.Timeout(5 * time.Second).Eval(`() => navigator.userAgent`); eerr == nil && v.Value.Str() != "" {
				ua = v.Value.Str()
			}
			return rodCookiesToHTTP(rodCookies), ua, nil
		}

		if time.Now().After(deadline) {
			return nil, "", fmt.Errorf("timeout waiting for cloudflare challenge clearance on %s", targetURL)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
