package where

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
)

// TestCoexistencePaths verifies isolated kaizen-mangal directories and
// KAIZEN_MANGAL_CONFIG_PATH / KAIZEN_MANGAL_CACHE_PATH / opt-in KAIZEN_MANGAL_SOURCES_PATH overrides.
func TestCoexistencePaths(t *testing.T) {
	filesystem.SetOsFs()

	// Default paths must use kaizen-mangal (not mangal).
	t.Setenv(EnvConfigPath, "")
	t.Setenv(EnvCachePath, "")
	t.Setenv(EnvSourcesPath, "")

	cfgDefault := Config()
	if filepath.Base(cfgDefault) != "kaizen-mangal" {
		t.Errorf("default Config() = %q, want base 'kaizen-mangal'", cfgDefault)
	}
	cacheDefault := Cache()
	if filepath.Base(cacheDefault) != "kaizen-mangal" {
		t.Errorf("default Cache() = %q, want base 'kaizen-mangal'", cacheDefault)
	}
	tempDefault := Temp()
	if !strings.HasPrefix(filepath.Base(tempDefault), "kaizen-mangal-") {
		t.Errorf("default Temp() = %q, want per-process prefix 'kaizen-mangal-'", tempDefault)
	}
	if !strings.HasPrefix(Sources(), cfgDefault) {
		t.Errorf("default Sources() = %q, want under Config() %q", Sources(), cfgDefault)
	}

	// Explicit overrides.
	customCfg := filepath.Join(t.TempDir(), "custom-cfg")
	customCache := filepath.Join(t.TempDir(), "custom-cache")
	customSources := filepath.Join(t.TempDir(), "shared-sources")

	t.Setenv(EnvConfigPath, customCfg)
	t.Setenv(EnvCachePath, customCache)
	t.Setenv(EnvSourcesPath, customSources)

	if got := Config(); filepath.Clean(got) != filepath.Clean(customCfg) {
		t.Errorf("Config() = %q, want %q", got, customCfg)
	}
	if got := Cache(); filepath.Clean(got) != filepath.Clean(customCache) {
		t.Errorf("Cache() = %q, want %q", got, customCache)
	}
	if got := Sources(); filepath.Clean(got) != filepath.Clean(customSources) {
		t.Errorf("Sources() = %q, want %q", got, customSources)
	}
}

func TestCleanStaleTempPreservesActiveSiblings(t *testing.T) {
	filesystem.SetOsFs()
	base := os.TempDir()

	staleDir := filepath.Join(base, "kaizen-mangal-999991-stale-test")
	activeDir := filepath.Join(base, "kaizen-mangal-999992-active-test")
	for _, d := range []string{staleDir, activeDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}

	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(staleDir, old, old); err != nil {
		t.Fatal(err)
	}

	CleanStaleTemp(time.Hour)

	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Errorf("expected stale temp dir %q to be removed", staleDir)
	}
	if _, err := os.Stat(activeDir); err != nil {
		t.Errorf("expected active sibling temp dir %q to remain intact, got err: %v", activeDir, err)
	}
}
