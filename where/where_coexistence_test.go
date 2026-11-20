package where

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
)

// TestCoexistencePaths verifies isolated kaizen-mangal directories and
// KAIZEN_MANGAL_CONFIG_PATH / KAIZEN_MANGAL_CACHE_PATH / opt-in KAIZEN_MANGAL_SOURCES_PATH overrides.
func TestCoexistencePaths(t *testing.T) {
	filesystem.SetOsFs()

	// Default paths must end in kaizen-mangal (not mangal).
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
	if filepath.Base(tempDefault) != "kaizen-mangal" {
		t.Errorf("default Temp() = %q, want base 'kaizen-mangal'", tempDefault)
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
