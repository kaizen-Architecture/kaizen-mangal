package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/kaizen-architecture/kaizen-mangal/where"
)

func makeTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 120, G: 80, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// TestKaizenContractGolden captures the offline CLI/JSON/config/file-naming contract
// required by Kaizen (RFC 0001 Section 6.F.2 & Section 14.4).
func TestKaizenContractGolden(t *testing.T) {
	binName := "kaizen-mangal-test"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), binName)
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(out))
	}

	pngBytes := makeTestPNG(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><a class="manga" href="/manga/golden">Golden</a></body></html>`)
	})
	mux.HandleFunc("/manga/golden", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><a class="chapter" href="/chapter/1">Chapter 1</a></body></html>`)
	})
	mux.HandleFunc("/chapter/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><img class="page" src="/img/1.png"/></body></html>`)
	})
	mux.HandleFunc("/img/1.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfgDir := t.TempDir()
	cacheDir := t.TempDir()
	libDir := t.TempDir()
	sourcesDir := filepath.Join(cfgDir, "sources")
	if err := os.MkdirAll(sourcesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fixtureLua := fmt.Sprintf(`
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
		table.insert(mangas, { name = s:text(), url = base .. s:attr("href") })
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
		table.insert(chapters, { name = s:text(), url = base .. s:attr("href"), volume = "1" })
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
		table.insert(pages, { url = base .. s:attr("src"), index = i })
	end)
	return pages
end
`, srv.URL)

	if err := os.WriteFile(filepath.Join(sourcesDir, "GoldenFixture.lua"), []byte(fixtureLua), 0o600); err != nil {
		t.Fatal(err)
	}

	runCLI := func(cwd string, args ...string) string {
		t.Helper()
		cmd := exec.Command(binPath, args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("%s=%s", where.EnvConfigPath, cfgDir),
			"XDG_CACHE_HOME="+cacheDir,
			"LOCALAPPDATA="+cacheDir,
		)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("cli %v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
		}
		return stdout.String()
	}

	// 1. where -s and where --cache
	gotSources := strings.TrimSpace(runCLI(libDir, "where", "-s"))
	if filepath.Clean(gotSources) != filepath.Clean(sourcesDir) {
		t.Fatalf("where -s = %q, want %q", gotSources, sourcesDir)
	}
	gotCache := strings.TrimSpace(runCLI(libDir, "where", "--cache"))
	if gotCache == "" {
		t.Fatalf("where --cache returned empty string")
	}

	// 2. sources list -r
	sourcesOut := runCLI(libDir, "sources", "list", "-r")
	foundFixture := false
	for _, line := range strings.Split(sourcesOut, "\n") {
		if strings.TrimSpace(line) == "GoldenFixture" {
			foundFixture = true
			break
		}
	}
	if !foundFixture {
		t.Fatalf("sources list -r did not contain GoldenFixture:\n%s", sourcesOut)
	}

	// 3. config info -j schema & allowed types + excludedConfigs presence
	cfgInfoOut := runCLI(libDir, "config", "info", "-j")
	var cfgItems []struct {
		Key         string `json:"key"`
		Value       any    `json:"value"`
		Default     any    `json:"default"`
		Description string `json:"description"`
		Type        string `json:"type"`
	}
	if err := json.Unmarshal([]byte(cfgInfoOut), &cfgItems); err != nil {
		t.Fatalf("config info -j invalid JSON: %v\n%s", err, cfgInfoOut)
	}
	allowedTypes := map[string]bool{"bool": true, "int": true, "string": true, "[]string": true}
	presentKeys := make(map[string]bool, len(cfgItems))
	for _, item := range cfgItems {
		if !allowedTypes[item.Type] {
			t.Errorf("config key %q has disallowed type %q", item.Key, item.Type)
		}
		presentKeys[item.Key] = true
	}
	for _, reqKey := range []string{
		"downloader.chapter_name_template",
		"downloader.redownload_existing",
		"downloader.download_cover",
		"downloader.create_manga_dir",
		"downloader.create_volume_dir",
		"downloader.default_sources",
		"downloader.path",
		"downloader.stop_on_error",
		"metadata.comic_info_xml",
		"metadata.fetch_anilist",
		"metadata.series_json",
		"formats.use",
	} {
		if !presentKeys[reqKey] {
			t.Errorf("required Kaizen config key %q missing from config info -j", reqKey)
		}
	}

	// 4. config set + write + get round-trip
	runCLI(libDir, "config", "set", "--key", "formats.use", "--value", "cbz")
	runCLI(libDir, "config", "set", "--key", "metadata.fetch_anilist", "--value", "false")
	gotFormat := strings.TrimSpace(runCLI(libDir, "config", "get", "-k", "formats.use"))
	if gotFormat != "cbz" {
		t.Fatalf("config get formats.use = %q, want cbz", gotFormat)
	}

	// 5. inline -j JSON contract (including "mangal" key)
	inlineOut := runCLI(libDir, "inline", "--source", "GoldenFixture", "--query", "Golden", "--chapters", "all", "-j")
	var parsed struct {
		Query  string `json:"query"`
		Result []struct {
			Source string `json:"source"`
			Mangal struct {
				Name     string `json:"name"`
				URL      string `json:"url"`
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Chapters []struct {
					Name   string `json:"name"`
					URL    string `json:"url"`
					Index  int    `json:"index"`
					ID     string `json:"id"`
					Volume string `json:"volume"`
				} `json:"chapters"`
			} `json:"mangal"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(inlineOut), &parsed); err != nil {
		t.Fatalf("inline -j invalid JSON: %v\n%s", err, inlineOut)
	}
	if len(parsed.Result) != 1 || parsed.Result[0].Source != "GoldenFixture" || parsed.Result[0].Mangal.Name != "Golden" {
		t.Fatalf("unexpected inline -j result: %+v", parsed)
	}
	if len(parsed.Result[0].Mangal.Chapters) != 1 || parsed.Result[0].Mangal.Chapters[0].Name != "Chapter 1" {
		t.Fatalf("unexpected inline -j chapters: %+v", parsed.Result[0].Mangal.Chapters)
	}

	// 6. inline -d --format cbz download contract + file naming invariant
	dlOut := strings.TrimSpace(runCLI(libDir, "inline", "--source", "GoldenFixture", "--query", "Golden", "--manga", "exact", "--chapters", "0", "-d", "--format", "cbz"))
	if !filepath.IsAbs(dlOut) {
		t.Fatalf("expected absolute .cbz path on stdout, got %q", dlOut)
	}
	if filepath.Ext(dlOut) != ".cbz" {
		t.Fatalf("expected .cbz extension, got %q", dlOut)
	}
	if _, err := os.Stat(dlOut); err != nil {
		t.Fatalf("downloaded file does not exist at %q: %v", dlOut, err)
	}

	// Verify Kaizen's getChapterIndexFromFile regex recovers index 0 from [0001]
	baseName := filepath.Base(dlOut)
	re := regexp.MustCompile(`\[(\d+)\]`)
	m := re.FindStringSubmatch(baseName)
	if len(m) < 2 {
		t.Fatalf("downloaded filename %q does not match [NNNN] pattern", baseName)
	}
	idx, err := strconv.Atoi(m[1])
	if err != nil || idx-1 != 0 {
		t.Fatalf("recovered index from %q = %d, want 0", baseName, idx-1)
	}
}
