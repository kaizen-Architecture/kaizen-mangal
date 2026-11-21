package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/network"
	"github.com/kaizen-architecture/kaizen-mangal/where"
)

func buildTestBinary(t *testing.T) string {
	t.Helper()
	binName := "kaizen-mangal-p6-test"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), binName)
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(out))
	}
	return binPath
}

func runWithEnv(binPath, cwd string, extraEnv []string, args ...string) (string, string, int) {
	cmd := exec.Command(binPath, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

// TestPhase6KaizenFacingCommands verifies version --json, sources validate --json,
// doctor --json (warnings a & b), breaker-open exit code 75, and mid-run cache wipe tolerance.
func TestPhase6KaizenFacingCommands(t *testing.T) {
	binPath := buildTestBinary(t)
	cfgDir := t.TempDir()
	cacheDir := t.TempDir()
	workDir := t.TempDir()
	baseEnv := []string{
		fmt.Sprintf("%s=%s", where.EnvConfigPath, cfgDir),
		fmt.Sprintf("%s=%s", where.EnvCachePath, cacheDir),
		"KAIZEN_MANGAL_DISABLE_UPDATE_CHECK=1",
	}

	// 1. version --json
	t.Run("version_json", func(t *testing.T) {
		stdout, stderr, code := runWithEnv(binPath, workDir, baseEnv, "version", "--json")
		if code != 0 {
			t.Fatalf("version --json exited %d: stderr=%s", code, stderr)
		}
		var v struct {
			App             string `json:"app"`
			Version         string `json:"version"`
			Commit          string `json:"commit"`
			OS              string `json:"os"`
			Arch            string `json:"arch"`
			ContractVersion int    `json:"contract_version"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &v); err != nil {
			t.Fatalf("invalid version --json output %q: %v", stdout, err)
		}
		if v.App != "kaizen-mangal" {
			t.Errorf("app = %q, want kaizen-mangal", v.App)
		}
		if v.Version == "" {
			t.Errorf("version is empty")
		}
		if v.OS != runtime.GOOS || v.Arch != runtime.GOARCH {
			t.Errorf("os/arch = %s/%s, want %s/%s", v.OS, v.Arch, runtime.GOOS, runtime.GOARCH)
		}
		if v.ContractVersion != network.ContractVersion {
			t.Errorf("contract_version = %d, want %d", v.ContractVersion, network.ContractVersion)
		}
	})

	// 2. sources validate --json
	t.Run("sources_validate_json", func(t *testing.T) {
		validFile := filepath.Join(workDir, "ValidSource.lua")
		if err := os.WriteFile(validFile, []byte(`
function SearchManga(q) return {} end
function MangaChapters(u) return {} end
function ChapterPages(u) return {} end
`), 0o600); err != nil {
			t.Fatal(err)
		}

		stdout, _, code := runWithEnv(binPath, workDir, baseEnv, "sources", "validate", validFile, "--json")
		if code != 0 {
			t.Fatalf("expected exit 0 for valid source, got %d: %s", code, stdout)
		}
		var res struct {
			Valid            bool `json:"valid"`
			Name             string `json:"name"`
			Errors           []struct {
				Kind    string `json:"kind"`
				Message string `json:"message"`
				Line    int    `json:"line"`
			} `json:"errors"`
			MissingFunctions []string `json:"missing_functions"`
			UsesHeadless     bool     `json:"uses_headless"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &res); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, stdout)
		}
		if !res.Valid || res.Name != "ValidSource" || res.UsesHeadless || len(res.MissingFunctions) != 0 || len(res.Errors) != 0 {
			t.Fatalf("unexpected valid result: %+v", res)
		}

		// Syntax error with line number
		syntaxFile := filepath.Join(workDir, "BrokenSyntax.lua")
		if err := os.WriteFile(syntaxFile, []byte("function SearchManga(q)\n  local x = (\nend\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, _, code = runWithEnv(binPath, workDir, baseEnv, "sources", "validate", syntaxFile, "--json")
		if code != 1 {
			t.Fatalf("expected exit 1 for syntax error, got %d: %s", code, stdout)
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &res); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, stdout)
		}
		if res.Valid || len(res.Errors) == 0 || res.Errors[0].Kind != "syntax" || res.Errors[0].Line == 0 {
			t.Fatalf("expected syntax error with line number, got: %+v", res)
		}

		// Missing function + uses_headless + top-level http/headless calls must be stubbed (no network/browser)
		hitNetwork := false
		neverHit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hitNetwork = true
		}))
		defer neverHit.Close()

		stubFile := filepath.Join(workDir, "HeadlessMissing.lua")
		stubSrc := fmt.Sprintf(`
local http = require("http")
local headless = require("headless")
local _ = http.get(%q)
local b = headless.browser()
local p = b:page()
p:navigate(%q)
function SearchManga(q) return {} end
function MangaChapters(u) return {} end
`, neverHit.URL, neverHit.URL)
		if err := os.WriteFile(stubFile, []byte(stubSrc), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, _, code = runWithEnv(binPath, workDir, baseEnv, "sources", "validate", stubFile, "--json")
		if code != 1 {
			t.Fatalf("expected exit 1 for missing function, got %d: %s", code, stdout)
		}
		if hitNetwork {
			t.Fatalf("sources validate executed a real HTTP request during validation")
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &res); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, stdout)
		}
		if res.Valid || !res.UsesHeadless || len(res.MissingFunctions) != 1 || res.MissingFunctions[0] != "ChapterPages" {
			t.Fatalf("unexpected missing/headless result: %+v", res)
		}
	})

	// 3. doctor --json (both warning (a) Chrome mismatch and warning (b) fallback without browser)
	t.Run("doctor_json_warnings", func(t *testing.T) {
		mockSolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"msg":"FlareSolverr is ready!","version":"3.3.21","userAgent":"Mozilla/5.0 Chrome/120.0.0.0 Safari/537.36"}`))
		}))
		defer mockSolver.Close()

		env := append(append([]string{}, baseEnv...),
			"KAIZEN_MANGAL_NETWORK_IMPERSONATE=chrome_133",
			"KAIZEN_MANGAL_NETWORK_CLOUDFLARE_SOLVER_URL="+mockSolver.URL+"/v1",
			"KAIZEN_MANGAL_NETWORK_CLOUDFLARE_SOLVER_FALLBACK=true",
			"KAIZEN_MANGAL_HEADLESS_BIN="+filepath.Join(workDir, "does-not-exist-chrome"),
			"KAIZEN_MANGAL_HEADLESS_ALLOW_DOWNLOAD=false",
		)
		stdout, stderr, code := runWithEnv(binPath, workDir, env, "doctor", "--json")
		if code != 0 {
			t.Fatalf("doctor --json exited %d: stderr=%s", code, stderr)
		}
		var doc struct {
			ContractVersion int `json:"contract_version"`
			OK              bool `json:"ok"`
			TLSProfile      struct {
				Resolved    string `json:"resolved"`
				ChromeMajor int    `json:"chrome_major"`
			} `json:"tls_profile"`
			Browser struct {
				Available bool   `json:"available"`
				Mode      string `json:"mode"`
			} `json:"browser"`
			Solver struct {
				Configured  bool `json:"configured"`
				Reachable   bool `json:"reachable"`
				ChromeMajor int  `json:"chrome_major"`
			} `json:"solver"`
			Storage struct {
				CacheWritable     bool `json:"cache_writable"`
				CookieJarWritable bool `json:"cookie_jar_writable"`
			} `json:"storage"`
			Warnings []string `json:"warnings"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &doc); err != nil {
			t.Fatalf("invalid doctor --json output: %v\n%s", err, stdout)
		}
		if doc.ContractVersion != network.ContractVersion {
			t.Errorf("contract_version = %d, want %d", doc.ContractVersion, network.ContractVersion)
		}
		if !doc.OK || !doc.Solver.Reachable || doc.Solver.ChromeMajor != 120 || doc.TLSProfile.ChromeMajor != 133 {
			t.Errorf("unexpected doctor solver/profile state: %+v", doc)
		}
		if doc.Browser.Available || doc.Browser.Mode != "unavailable" {
			t.Errorf("expected unavailable browser, got %+v", doc.Browser)
		}
		if !doc.Storage.CacheWritable || !doc.Storage.CookieJarWritable {
			t.Errorf("expected writable cache and cookie jar, got %+v", doc.Storage)
		}

		joined := strings.Join(doc.Warnings, "\n")
		if !strings.Contains(joined, "Chrome 133") || !strings.Contains(joined, "120") {
			t.Errorf("expected Chrome major mismatch warning, got warnings: %v", doc.Warnings)
		}
		if !strings.Contains(joined, "cloudflare_solver_fallback") {
			t.Errorf("expected fallback-without-browser warning, got warnings: %v", doc.Warnings)
		}
	})

	// 4. Breaker-open exit code 75 + [kaizen-mangal:breaker-open] + no retryable keywords
	t.Run("breaker_open_exit_75", func(t *testing.T) {
		cfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Server", "cloudflare")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<html><title>Just a moment...</title></html>`))
		}))
		defer cfServer.Close()

		sourcesDir := filepath.Join(cfgDir, "sources")
		if err := os.MkdirAll(sourcesDir, 0o755); err != nil {
			t.Fatal(err)
		}
		srcLua := fmt.Sprintf(`
local http = require("http")
local client = http.client()
function SearchManga(q)
	local req = http.request("GET", %q)
	local res, err = client:do_request(req)
	if err then error(err) end
	return {}
end
function MangaChapters(u) return {} end
function ChapterPages(u) return {} end
`, cfServer.URL)
		if err := os.WriteFile(filepath.Join(sourcesDir, "CFBreakerTest.lua"), []byte(srcLua), 0o600); err != nil {
			t.Fatal(err)
		}

		// Pre-open the breaker for the test server's host in the isolated cache dir.
		host := strings.TrimPrefix(cfServer.URL, "http://")
		host = strings.Split(host, ":")[0]
		breakerState := map[string]any{
			host: map[string]any{
				"failures":   2,
				"open_until": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
			},
		}
		raw, _ := json.Marshal(breakerState)
		if err := os.WriteFile(filepath.Join(cacheDir, "kaizen-mangal-cf-breaker.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}

		stdout, stderr, code := runWithEnv(binPath, workDir, baseEnv, "inline", "--source", "CFBreakerTest", "--query", "test", "-j")
		if code != network.ExitCodeBreakerOpen {
			t.Fatalf("expected exit code %d, got %d\nstdout=%s\nstderr=%s", network.ExitCodeBreakerOpen, code, stdout, stderr)
		}
		combined := stdout + "\n" + stderr
		if !strings.Contains(combined, network.BreakerOpenMarker) {
			t.Errorf("expected %q in output, got:\n%s", network.BreakerOpenMarker, combined)
		}
		if !strings.Contains(stderr, `[kaizen-mangal:error]`) || !strings.Contains(stderr, `"code":"breaker_open"`) {
			t.Errorf("expected structured breaker_open error line on stderr, got:\n%s", stderr)
		}
		// Strip the random httptest.Server URL/port (e.g. :42953) before checking for forbidden HTTP status codes.
		sanitized := strings.ReplaceAll(combined, cfServer.URL, "<test-url>")
		for _, forbidden := range []string{"503", "429", "timeout", "connection refused"} {
			if strings.Contains(strings.ToLower(sanitized), forbidden) {
				t.Errorf("breaker-open output must not contain retryable keyword %q:\n%s", forbidden, combined)
			}
		}
	})

	// 5. Mid-run where.Cache() wipe tolerance (simulating Kaizen clearCache())
	t.Run("mid_run_cache_wipe_tolerance", func(t *testing.T) {
		pngBytes := makeTestPNG(t)
		wipeCacheDir := t.TempDir()
		mux := http.NewServeMux()
		mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
			// Simulate Kaizen clearCache() wiping non-anilist files mid-run
			entries, _ := os.ReadDir(wipeCacheDir)
			for _, e := range entries {
				if !strings.Contains(strings.ToLower(e.Name()), "anilist") {
					_ = os.RemoveAll(filepath.Join(wipeCacheDir, e.Name()))
				}
			}
			w.Header().Set("Set-Cookie", "session=ok; Path=/")
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><a class="m" href="/m">WipeTest</a></body></html>`)
		})
		mux.HandleFunc("/m", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `<html><body><a class="c" href="/c">Chapter 1</a></body></html>`)
		})
		mux.HandleFunc("/c", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `<html><body><img class="p" src="/p.png"/></body></html>`)
		})
		mux.HandleFunc("/p.png", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		sourcesDir := filepath.Join(cfgDir, "sources")
		_ = os.MkdirAll(sourcesDir, 0o755)
		luaSrc := fmt.Sprintf(`
local http = require("http")
local html = require("html")
local client = http.client()
local base = %q
function SearchManga(q)
	local res, err = client:do_request(http.request("GET", base .. "/search"))
	if err then error(err) end
	local doc = html.parse(res.body)
	local out = {}
	doc:find("a.m"):each(function(i, s) table.insert(out, {name=s:text(), url=base..s:attr("href")}) end)
	return out
end
function MangaChapters(u)
	local res, err = client:do_request(http.request("GET", u))
	if err then error(err) end
	local doc = html.parse(res.body)
	local out = {}
	doc:find("a.c"):each(function(i, s) table.insert(out, {name=s:text(), url=base..s:attr("href")}) end)
	return out
end
function ChapterPages(u)
	local res, err = client:do_request(http.request("GET", u))
	if err then error(err) end
	local doc = html.parse(res.body)
	local out = {}
	doc:find("img.p"):each(function(i, s) table.insert(out, {url=base..s:attr("src"), index=i}) end)
	return out
end
`, srv.URL)
		if err := os.WriteFile(filepath.Join(sourcesDir, "WipeFixture.lua"), []byte(luaSrc), 0o600); err != nil {
			t.Fatal(err)
		}

		// Pre-populate cache with state files before the wipe happens mid-run
		_ = os.WriteFile(filepath.Join(wipeCacheDir, "kaizen-mangal-cookies.json"), []byte(`{}`), 0o600)
		_ = os.WriteFile(filepath.Join(wipeCacheDir, "kaizen-mangal-cookies.json.lock"), []byte(``), 0o600)

		env := []string{
			fmt.Sprintf("%s=%s", where.EnvConfigPath, cfgDir),
			fmt.Sprintf("%s=%s", where.EnvCachePath, wipeCacheDir),
			"KAIZEN_MANGAL_DISABLE_UPDATE_CHECK=1",
			"KAIZEN_MANGAL_METADATA_FETCH_ANILIST=false",
		}
		libDir := t.TempDir()
		stdout, stderr, code := runWithEnv(binPath, libDir, env, "inline", "--source", "WipeFixture", "--query", "WipeTest", "--manga", "exact", "--chapters", "0", "-d", "--format", "cbz")
		if code != 0 {
			t.Fatalf("mid-run cache wipe caused download failure (exit %d):\nstdout=%s\nstderr=%s", code, stdout, stderr)
		}
		dlPath := strings.TrimSpace(stdout)
		if !filepath.IsAbs(dlPath) || filepath.Ext(dlPath) != ".cbz" {
			t.Fatalf("unexpected download output after cache wipe: %q", dlPath)
		}
	})
}
