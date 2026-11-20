package where

import (
	"os"
	"path/filepath"

	"github.com/kaizen-architecture/kaizen-mangal/constant"
	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/samber/lo"
	"github.com/spf13/viper"
)

const (
	EnvConfigPath  = "KAIZEN_MANGAL_CONFIG_PATH"
	EnvCachePath   = "KAIZEN_MANGAL_CACHE_PATH"
	EnvSourcesPath = "KAIZEN_MANGAL_SOURCES_PATH"
)

// mkdir creates a directory and all parent directories if they don't exist
// will return the path of the directory
func mkdir(path string) string {
	lo.Must0(filesystem.Api().MkdirAll(path, os.ModePerm))
	return path
}

// Config path
// Will create the directory if it doesn't exist
func Config() string {
	var path string

	if customDir, present := os.LookupEnv(EnvConfigPath); present && customDir != "" {
		path = customDir
	} else {
		path = filepath.Join(lo.Must(os.UserConfigDir()), constant.Mangal)
	}

	return mkdir(path)
}

// Sources path
// Will create the directory if it doesn't exist.
// Isolated under Config()/sources by default; opt-in override via KAIZEN_MANGAL_SOURCES_PATH.
func Sources() string {
	if customDir, present := os.LookupEnv(EnvSourcesPath); present && customDir != "" {
		return mkdir(customDir)
	}
	return mkdir(filepath.Join(Config(), "sources"))
}

func AnilistBinds() string {
	return filepath.Join(Config(), "anilist.json")
}

// Logs path
// Will create the directory if it doesn't exist
func Logs() string {
	return mkdir(filepath.Join(Config(), "logs"))
}

// Queries path
// Will create the directory if it doesn't exist
func Queries() string {
	return filepath.Join(Cache(), "queries.json")
}

// History path to the file
// Will create the directory if it doesn't exist
func History() string {
	return filepath.Join(Config(), "history.json")
}

// Downloads path
// Will create the directory if it doesn't exist
func Downloads() string {
	path, err := filepath.Abs(viper.GetString(key.DownloaderPath))

	if err != nil {
		path, err = os.Getwd()
		if err != nil {
			path = "."
		}
	}

	return mkdir(path)
}

// Cache path
// Will create the directory if it doesn't exist
func Cache() string {
	if customDir, present := os.LookupEnv(EnvCachePath); present && customDir != "" {
		return mkdir(customDir)
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = filepath.Join(".", "cache")
	}

	cacheDir = filepath.Join(cacheDir, constant.Mangal)
	return mkdir(cacheDir)
}

// Temp path
// Will create the directory if it doesn't exist
func Temp() string {
	tempDir := filepath.Join(os.TempDir(), constant.Mangal)
	return mkdir(tempDir)
}
