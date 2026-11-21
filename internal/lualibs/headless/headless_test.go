package headless

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/spf13/viper"
)

func TestResolveBrowserOrder(t *testing.T) {
	t.Cleanup(viper.Reset)

	// 1. Remote CDP takes highest priority
	viper.Reset()
	viper.Set(key.HeadlessCDPURL, "ws://127.0.0.1:9222/devtools/browser/test")
	res := ResolveBrowser()
	if res.Mode != "cdp" || res.Target != "ws://127.0.0.1:9222/devtools/browser/test" {
		t.Fatalf("expected cdp resolution, got %+v", res)
	}

	// 2. Explicit binary (existing file) takes second priority
	viper.Reset()
	fakeBin := filepath.Join(t.TempDir(), "fake-chromium")
	if err := os.WriteFile(fakeBin, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	viper.Set(key.HeadlessBin, fakeBin)
	res = ResolveBrowser()
	if res.Mode != "bin" || res.Target != fakeBin {
		t.Fatalf("expected bin resolution, got %+v", res)
	}

	// 3. Explicit binary pointing to missing file -> unavailable with clear error
	viper.Reset()
	viper.Set(key.HeadlessBin, filepath.Join(t.TempDir(), "does-not-exist"))
	res = ResolveBrowser()
	if res.Mode != "unavailable" || res.Error == "" {
		t.Fatalf("expected unavailable for missing bin, got %+v", res)
	}

	// 4. SolveCloudflareChallenge returns clean error when unavailable (never panics)
	_, _, err := SolveCloudflareChallenge(context.Background(), "https://example.org", 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "headless.cdp_url") {
		t.Fatalf("expected clear error mentioning headless.cdp_url, got: %v", err)
	}
}

func TestManagedBrowserCloseCleansOnlyItsOwnTempDir(t *testing.T) {
	ownDir := t.TempDir()
	siblingDir := t.TempDir()

	// Create a marker file inside siblingDir to ensure it is never touched
	marker := filepath.Join(siblingDir, "keep.txt")
	if err := os.WriteFile(marker, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	subOwn := filepath.Join(ownDir, "kaizen-mangal-rod-test")
	if err := os.MkdirAll(subOwn, 0o755); err != nil {
		t.Fatal(err)
	}

	mb := &ManagedBrowser{UserDataDir: subOwn}
	trackBrowser(mb)

	// Calling Close twice must be idempotent and remove only subOwn
	_ = mb.Close()
	_ = mb.Close()

	if _, err := os.Stat(subOwn); !os.IsNotExist(err) {
		t.Errorf("expected own profile dir %q to be removed on Close()", subOwn)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("sibling dir marker %q must remain untouched: %v", marker, err)
	}
}
