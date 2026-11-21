package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/where"
	. "github.com/smartystreets/goconvey/convey"
)

func TestGet(t *testing.T) {
	filesystem.SetOsFs()
	cfgDir := t.TempDir()
	t.Setenv(where.EnvConfigPath, cfgDir)
	sourcesDir := filepath.Join(cfgDir, "sources")
	if err := os.MkdirAll(sourcesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := []byte("function SearchManga(q) return {} end\nfunction MangaChapters(u) return {} end\nfunction ChapterPages(u) return {} end\n")
	if err := os.WriteFile(filepath.Join(sourcesDir, "MangaDex.lua"), stub, 0o600); err != nil {
		t.Fatal(err)
	}

	Convey("When checking built-in providers after Phase 8 retirement", t, func() {
		So(len(Builtins()), ShouldEqual, 0)
	})

	Convey("When trying to get a valid custom Lua provider (exact and case-insensitive)", t, func() {
		p, ok := Get("MangaDex")
		Convey("Then exact match should succeed", func() {
			So(ok, ShouldBeTrue)
			So(p.Name, ShouldEqual, "MangaDex")
		})

		pCi, okCi := Get("Mangadex")
		Convey("Then case-insensitive match should resolve to custom Lua provider", func() {
			So(okCi, ShouldBeTrue)
			So(pCi.Name, ShouldEqual, "MangaDex")
		})
	})

	Convey("When trying to get an invalid provider", t, func() {
		_, ok := Get("kek")
		Convey("Then ok should be false", func() {
			So(ok, ShouldBeFalse)
		})
	})
}
