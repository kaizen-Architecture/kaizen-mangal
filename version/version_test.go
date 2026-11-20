package version

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/kaizen-architecture/kaizen-mangal/constant"
	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/where"
)

func TestLatestVersion(t *testing.T) {
	filesystem.SetMemMapFs()
	t.Cleanup(filesystem.SetOsFs)
	t.Setenv(where.EnvCachePath, "/mem-cache")

	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v1.2.3"}`)
	}))
	defer srv.Close()

	prevURL := releasesAPIURL
	releasesAPIURL = srv.URL
	t.Cleanup(func() { releasesAPIURL = prevURL })

	ver, err := Latest()
	if err != nil {
		t.Fatalf("Latest() returned error: %v", err)
	}
	if ver != "1.2.3" {
		t.Fatalf("Latest() = %q, want %q", ver, "1.2.3")
	}

	semverRegex := regexp.MustCompile(`^v?(\d+)(\.\d+){0,2}(-\w+)?$`)
	if !semverRegex.MatchString(ver) {
		t.Fatalf("version %q does not match semver", ver)
	}
	if gotUA != constant.AppUserAgent || !strings.HasPrefix(gotUA, "kaizen-mangal/") {
		t.Fatalf("User-Agent = %q, want %q", gotUA, constant.AppUserAgent)
	}
}
