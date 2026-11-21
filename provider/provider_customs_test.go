package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/where"
)

// TestCustomsNonRecursiveDisabledDirs locks the Pillar A.6 invariant:
// provider.Customs() reads only top-level *.lua files and ignores disabled/ and disabled/failed/.
func TestCustomsNonRecursiveDisabledDirs(t *testing.T) {
	filesystem.SetOsFs()

	cfgDir := t.TempDir()
	t.Setenv(where.EnvConfigPath, cfgDir)

	sourcesDir := filepath.Join(cfgDir, "sources")
	disabledDir := filepath.Join(sourcesDir, "disabled")
	failedDir := filepath.Join(disabledDir, "failed")
	for _, dir := range []string{sourcesDir, disabledDir, failedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	stub := []byte("function SearchManga(q) return {} end\nfunction MangaChapters(u) return {} end\nfunction ChapterPages(u) return {} end\n")
	if err := os.WriteFile(filepath.Join(sourcesDir, "active.lua"), stub, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disabledDir, "parked.lua"), stub, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(failedDir, "broken.lua"), stub, 0o600); err != nil {
		t.Fatal(err)
	}

	customs := Customs()
	if len(customs) != 1 {
		t.Fatalf("expected exactly 1 active custom provider, got %d: %+v", len(customs), customs)
	}
	if customs[0].Name != "active" {
		t.Fatalf("expected provider name 'active', got %q", customs[0].Name)
	}
}
