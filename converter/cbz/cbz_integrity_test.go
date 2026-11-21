package cbz

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/source"
)

func TestSaveToAtomicFailClosed(t *testing.T) {
	filesystem.SetOsFs()
	t.Cleanup(filesystem.SetMemMapFs)

	dir := t.TempDir()
	target := filepath.Join(dir, "chapter.cbz")

	// Zero pages -> error, no file created
	if err := SaveTo(&source.Chapter{Name: "Empty"}, target); err == nil {
		t.Fatal("expected error for zero-page chapter")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("final file %q must not exist after failed write", target)
	}

	// Nil/empty page contents -> error, no final file and no .kaizen-tmp leftover
	badChapter := &source.Chapter{
		Name: "Broken",
		Pages: []*source.Page{
			{Index: 1, Extension: ".png", Contents: bytes.NewBuffer([]byte{0x89, 0x50, 0x4E, 0x47})},
			{Index: 2, Extension: ".png", Contents: nil},
		},
	}
	if err := SaveTo(badChapter, target); err == nil {
		t.Fatal("expected error when page #2 has nil contents")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("final file %q must not exist after partial failure", target)
	}
	if _, err := os.Stat(target + ".kaizen-tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file %q must be cleaned up after failure", target+".kaizen-tmp")
	}
}
