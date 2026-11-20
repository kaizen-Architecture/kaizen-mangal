package installer

import (
	"errors"
	"path/filepath"

	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/kaizen-architecture/kaizen-mangal/util"
	"github.com/samber/lo"
	"github.com/spf13/viper"
)

var collector *githubFilesCollector

// Scrapers gets available scrapers from a user-configured GitHub repo.
func Scrapers() ([]*Scraper, error) {
	if viper.GetString(key.InstallerUser) == "" || viper.GetString(key.InstallerRepo) == "" {
		return nil, errors.New("installer.user and installer.repo are not configured")
	}

	if collector == nil {
		setupCollector()
	}

	err := collector.collect()
	if err != nil {
		return nil, err
	}

	return lo.FilterMap(collector.Files, func(f *GithubFile, _ int) (*Scraper, bool) {
		if filepath.Ext(f.Path) != ".lua" {
			return nil, false
		}

		return &Scraper{
			Name: util.FileStem(filepath.Base(f.Path)),
			URL:  f.Url,
		}, true
	}), nil
}

func setupCollector() {
	collector = &githubFilesCollector{
		user:   viper.GetString(key.InstallerUser),
		repo:   viper.GetString(key.InstallerRepo),
		branch: viper.GetString(key.InstallerBranch),
	}
}
