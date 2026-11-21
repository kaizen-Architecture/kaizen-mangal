package custom

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	lua "github.com/yuin/gopher-lua"
)

// TestLuaPreloadedModulesConformance captures the baseline Lua module surface
// required by RFC 0001 (Pillar A.5 & Section 14.3).
func TestLuaPreloadedModulesConformance(t *testing.T) {
	filesystem.SetOsFs()

	modules := []string{
		"base64",
		"crypto",
		"filepath",
		"goos",
		"headless",
		"html",
		"http",
		"humanize",
		"inspect",
		"ioutil",
		"json",
		"log",
		"regexp",
		"runtime",
		"shellescape",
		"stats",
		"storage",
		"strings",
		"template",
		"time",
		"xmlpath",
		"yaml",
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "conformance.lua")

	script := `
local checks = {}
function CheckModule(name)
	local ok, mod = pcall(require, name)
	if not ok or mod == nil then
		error("failed to require module: " .. name .. " -> " .. tostring(mod))
	end
	return type(mod)
end

function SearchManga(query) return {} end
function MangaChapters(url) return {} end
function ChapterPages(url) return {} end
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write conformance lua: %v", err)
	}

	src, err := LoadSource(scriptPath, true)
	if err != nil {
		t.Fatalf("LoadSource failed: %v", err)
	}
	ls := src.(*luaSource)

	for _, mod := range modules {
		val, err := ls.call("CheckModule", lua.LTString, lua.LString(mod))
		if err != nil {
			t.Errorf("module %q failed conformance check: %v", mod, err)
			continue
		}
		if val.String() != "table" {
			t.Errorf("module %q expected type table, got %q", mod, val.String())
		}
	}
}

// TestLuaFixtureScraperEndToEnd captures the baseline SearchManga, MangaChapters
// and ChapterPages contract against a local httptest.Server.
func TestLuaFixtureScraperEndToEnd(t *testing.T) {
	filesystem.SetOsFs()
	cacheDir, err := os.MkdirTemp("", "kaizen-lua-cache-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cacheDir) })
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("LOCALAPPDATA", cacheDir)

	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><a class="manga" href="/manga/test-1">Test Manga</a></body></html>`)
	})
	mux.HandleFunc("/manga/test-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><a class="chapter" href="/chapter/1">Chapter 1</a></body></html>`)
	})
	mux.HandleFunc("/chapter/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><img class="page" src="/img/1.png"/></body></html>`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fixture.lua")
	script := fmt.Sprintf(`
local http = require("http")
local html = require("html")
local client = http.client()
local base = %q

function SearchManga(query)
	local req = http.request("GET", base .. "/search?q=" .. query)
	local res, err = client:do_request(req)
	if err then error(err) end
	local doc = html.parse(res.body)
	local mangas = {}
	doc:find("a.manga"):each(function(i, s)
		table.insert(mangas, {
			name = s:text(),
			url = base .. s:attr("href"),
		})
	end)
	return mangas
end

function MangaChapters(mangaURL)
	local req = http.request("GET", mangaURL)
	local res, err = client:do_request(req)
	if err then error(err) end
	local doc = html.parse(res.body)
	local chapters = {}
	doc:find("a.chapter"):each(function(i, s)
		table.insert(chapters, {
			name = s:text(),
			url = base .. s:attr("href"),
			volume = "1",
		})
	end)
	return chapters
end

function ChapterPages(chapterURL)
	local req = http.request("GET", chapterURL)
	local res, err = client:do_request(req)
	if err then error(err) end
	local doc = html.parse(res.body)
	local pages = {}
	doc:find("img.page"):each(function(i, s)
		table.insert(pages, {
			url = base .. s:attr("src"),
			index = i,
		})
	end)
	return pages
end
`, srv.URL)

	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write fixture lua: %v", err)
	}

	src, err := LoadSource(scriptPath, true)
	if err != nil {
		t.Fatalf("LoadSource failed: %v", err)
	}

	mangas, err := src.Search("Test")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(mangas) != 1 || mangas[0].Name != "Test Manga" {
		t.Fatalf("unexpected mangas: %+v", mangas)
	}

	chapters, err := src.ChaptersOf(mangas[0])
	if err != nil {
		t.Fatalf("ChaptersOf failed: %v", err)
	}
	if len(chapters) != 1 || chapters[0].Name != "Chapter 1" || chapters[0].Volume != "1" {
		t.Fatalf("unexpected chapters: %+v", chapters)
	}

	pages, err := src.PagesOf(chapters[0])
	if err != nil {
		t.Fatalf("PagesOf failed: %v", err)
	}
	if len(pages) != 1 || pages[0].URL != srv.URL+"/img/1.png" {
		t.Fatalf("unexpected pages: %+v", pages)
	}
}
