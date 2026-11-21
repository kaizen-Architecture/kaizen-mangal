package version

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/constant"
	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/util"
	"github.com/kaizen-architecture/kaizen-mangal/where"
	"github.com/metafates/gache"
)

const (
	EnvDisableUpdateCheck = "KAIZEN_MANGAL_DISABLE_UPDATE_CHECK"
	defaultReleasesAPIURL = "https://api.github.com/repos/kaizen-Architecture/kaizen-mangal/releases/latest"
	releasesPagePrefix    = "https://github.com/kaizen-Architecture/kaizen-mangal/releases/tag/v"
)

var (
	releasesAPIURL = defaultReleasesAPIURL
	httpClient     = &http.Client{Timeout: 5 * time.Second}
)

func newVersionCacher() *gache.Cache[string] {
	return gache.New[string](&gache.Options{
		Path:       filepath.Join(where.Cache(), "version.json"),
		Lifetime:   time.Hour * 24 * 2,
		FileSystem: &filesystem.GacheFs{},
	})
}

// Latest returns the latest version of kaizen-mangal.
// It fetches the latest version from the GitHub Releases API.
func Latest() (version string, err error) {
	cacher := newVersionCacher()
	ver, expired, err := cacher.Get()
	if err == nil && !expired && ver != "" {
		return ver, nil
	}

	req, err := http.NewRequest(http.MethodGet, releasesAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", constant.AppUserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer util.Ignore(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		return "", errors.New("unexpected status: " + resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}

	if err = json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	if release.TagName == "" {
		return "", errors.New("empty tag name")
	}

	version = strings.TrimPrefix(release.TagName, "v")
	_ = cacher.Set(version)
	return version, nil
}
