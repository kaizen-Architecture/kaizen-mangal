package headless

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/spf13/viper"
)

// BrowserResolution describes how kaizen-mangal resolves a Chromium/CDP backend (RFC 0001 Pillar D.4).
type BrowserResolution struct {
	Mode   string `json:"mode"`   // "cdp", "bin", "system", "download", or "unavailable"
	Target string `json:"target"` // CDP URL or executable path (empty for download/unavailable)
	Error  string `json:"error,omitempty"`
}

// DefaultTimeout returns the configured headless per-operation timeout (default 30s).
func DefaultTimeout() time.Duration {
	if sec := viper.GetInt(key.HeadlessTimeout); sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return 30 * time.Second
}

// ResolveBrowser resolves the browser backend in strict deterministic order:
// 1. headless.cdp_url (KAIZEN_MANGAL_HEADLESS_CDP_URL)
// 2. headless.bin (KAIZEN_MANGAL_HEADLESS_BIN) or CHROME_PATH
// 3. System discovery (launcher.LookPath)
// 4. Managed Rod download (only when headless.allow_download=true)
func ResolveBrowser() BrowserResolution {
	if cdp := strings.TrimSpace(viper.GetString(key.HeadlessCDPURL)); cdp != "" {
		return BrowserResolution{Mode: "cdp", Target: cdp}
	}

	binPath := strings.TrimSpace(viper.GetString(key.HeadlessBin))
	if binPath == "" {
		binPath = strings.TrimSpace(os.Getenv("CHROME_PATH"))
	}
	if binPath != "" {
		if info, err := os.Stat(binPath); err == nil && !info.IsDir() {
			return BrowserResolution{Mode: "bin", Target: binPath}
		}
		return BrowserResolution{
			Mode:   "unavailable",
			Target: binPath,
			Error:  fmt.Sprintf("configured headless binary %q does not exist", binPath),
		}
	}

	if found, ok := launcher.LookPath(); ok && found != "" {
		return BrowserResolution{Mode: "system", Target: found}
	}

	allowDownload := true
	if viper.IsSet(key.HeadlessAllowDownload) {
		allowDownload = viper.GetBool(key.HeadlessAllowDownload)
	}
	if allowDownload {
		return BrowserResolution{Mode: "download"}
	}

	return BrowserResolution{
		Mode:  "unavailable",
		Error: "no local Chromium/Chrome found and headless.allow_download is false (install chromium, set headless.bin, or set headless.cdp_url)",
	}
}

// ManagedBrowser owns a *rod.Browser, its isolated temporary user-data-dir, and cleanup lifecycle.
type ManagedBrowser struct {
	Browser     *rod.Browser
	UserDataDir string
	Timeout     time.Duration
	Resolution  BrowserResolution
	launcher    *launcher.Launcher
	closed      atomic.Bool
}

var (
	activeMu       sync.Mutex
	activeBrowsers = make(map[*ManagedBrowser]struct{})
)

func trackBrowser(mb *ManagedBrowser) {
	activeMu.Lock()
	activeBrowsers[mb] = struct{}{}
	activeMu.Unlock()
}

func untrackBrowser(mb *ManagedBrowser) {
	activeMu.Lock()
	delete(activeBrowsers, mb)
	activeMu.Unlock()
}

// CloseAllActive cleanly closes every ManagedBrowser spawned during this process.
// Called on CLI exit (main.go) so no orphan Chromium processes survive.
func CloseAllActive() {
	activeMu.Lock()
	list := make([]*ManagedBrowser, 0, len(activeBrowsers))
	for mb := range activeBrowsers {
		list = append(list, mb)
	}
	activeMu.Unlock()

	for _, mb := range list {
		_ = mb.Close()
	}
}

// Close closes the browser connection, terminates the child process (if locally launched),
// and removes ONLY this browser's isolated temporary profile directory.
func (mb *ManagedBrowser) Close() error {
	if mb == nil || !mb.closed.CompareAndSwap(false, true) {
		return nil
	}
	untrackBrowser(mb)

	var firstErr error
	if mb.Browser != nil {
		if err := mb.Browser.Close(); err != nil {
			firstErr = err
		}
	}
	if mb.launcher != nil {
		mb.launcher.Kill()
	}
	if mb.UserDataDir != "" {
		_ = os.RemoveAll(mb.UserDataDir)
	}
	return firstErr
}

// OpenBrowser launches or connects to a Chromium instance using ResolveBrowser()
// with container-safe flags and an isolated temp profile (never wiping /tmp/rod).
func OpenBrowser(timeout time.Duration) (*ManagedBrowser, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout()
	}

	res := ResolveBrowser()
	if res.Mode == "unavailable" {
		return nil, errors.New(res.Error)
	}

	if res.Mode == "cdp" {
		b := rod.New().ControlURL(res.Target)
		if err := b.Connect(); err != nil {
			return nil, fmt.Errorf("failed to connect to remote CDP %q: %w", res.Target, err)
		}
		mb := &ManagedBrowser{
			Browser:    b,
			Timeout:    timeout,
			Resolution: res,
		}
		trackBrowser(mb)
		return mb, nil
	}

	tmpDir, err := os.MkdirTemp("", "kaizen-mangal-rod-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create isolated headless profile dir: %w", err)
	}

	l := launcher.New().
		Headless(true).
		Leakless(false).
		NoSandbox(true).
		UserDataDir(tmpDir).
		Set("disable-dev-shm-usage").
		Set("disable-gpu").
		Set("disable-blink-features", "AutomationControlled")

	if res.Mode == "bin" || res.Mode == "system" {
		l = l.Bin(res.Target)
	}

	controlURL, err := l.Launch()
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to launch headless browser (%s): %w", res.Mode, err)
	}

	b := rod.New().ControlURL(controlURL)
	if err := b.Connect(); err != nil {
		l.Kill()
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to connect to headless browser: %w", err)
	}

	mb := &ManagedBrowser{
		Browser:     b,
		UserDataDir: tmpDir,
		Timeout:     timeout,
		Resolution:  res,
		launcher:    l,
	}
	trackBrowser(mb)
	return mb, nil
}
