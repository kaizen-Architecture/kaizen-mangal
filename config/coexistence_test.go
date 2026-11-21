package config

import (
	"strings"
	"testing"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/kaizen-architecture/kaizen-mangal/where"
	"github.com/spf13/viper"
)

// TestCoexistenceEnvPrefix verifies that kaizen-mangal reads KAIZEN_MANGAL_*
// exclusively and completely ignores upstream MANGAL_* variables.
func TestCoexistenceEnvPrefix(t *testing.T) {
	filesystem.SetOsFs()
	t.Setenv(where.EnvConfigPath, t.TempDir())

	// Ensure every Field.Env() uses KAIZEN_MANGAL_ prefix (never MANGAL_ or KAIZEN-MANGAL_).
	for _, field := range Default {
		env := field.Env()
		if !strings.HasPrefix(env, "KAIZEN_MANGAL_") {
			t.Errorf("field %q Env() = %q, want prefix KAIZEN_MANGAL_", field.Key, env)
		}
		if strings.Contains(env, "-") {
			t.Errorf("field %q Env() = %q contains invalid hyphen", field.Key, env)
		}
	}

	// Upstream MANGAL_* must be ignored.
	viper.Reset()
	t.Setenv("MANGAL_FORMATS_USE", "zip")
	t.Setenv("KAIZEN_MANGAL_FORMATS_USE", "")
	if err := Setup(); err != nil {
		t.Fatalf("Setup() failed: %v", err)
	}
	if got := viper.GetString(key.FormatsUse); got != "pdf" {
		t.Fatalf("formats.use = %q when only MANGAL_FORMATS_USE=zip is set; want default \"pdf\"", got)
	}

	// KAIZEN_MANGAL_* must be honored.
	viper.Reset()
	t.Setenv("KAIZEN_MANGAL_FORMATS_USE", "cbz")
	if err := Setup(); err != nil {
		t.Fatalf("Setup() failed: %v", err)
	}
	if got := viper.GetString(key.FormatsUse); got != "cbz" {
		t.Fatalf("formats.use = %q when KAIZEN_MANGAL_FORMATS_USE=cbz is set; want \"cbz\"", got)
	}
}
