package provider

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/provider/custom"
	"github.com/kaizen-architecture/kaizen-mangal/source"
	"github.com/kaizen-architecture/kaizen-mangal/util"
	"github.com/kaizen-architecture/kaizen-mangal/where"
	"github.com/samber/lo"
)

type Provider struct {
	ID           string
	Name         string
	UsesHeadless bool
	IsCustom     bool
	CreateSource func() (source.Source, error)
}

func (p Provider) String() string {
	return p.Name
}

func Builtins() []*Provider {
	return builtinProviders
}

func Customs() []*Provider {
	files, err := filesystem.Api().ReadDir(where.Sources())

	if err != nil {
		return make([]*Provider, 0)
	}

	paths := lo.FilterMap(files, func(f os.FileInfo, _ int) (string, bool) {
		if filepath.Ext(f.Name()) == CustomProviderExtension {
			return filepath.Join(where.Sources(), f.Name()), true
		}
		return "", false
	})
	providers := make([]*Provider, len(paths))

	for i, path := range paths {
		// Check if source contains line `require("headless")`
		// if so, set UsesHeadless to true.
		// This approach is not ideal, but it's the only way to do it without
		// actually loading the source.
		usesHeadless, _ := filesystem.Api().FileContainsAnyBytes(path, [][]byte{
			[]byte("require(\"headless\")"),
			[]byte("require('headless')"),
			[]byte("require(headless)"),
			[]byte("require'headless'"),
		})

		name := util.FileStem(path)
		path := path
		providers[i] = &Provider{
			ID:           custom.IDfromName(name),
			UsesHeadless: usesHeadless,
			IsCustom:     true,
			Name:         name,
			CreateSource: func() (source.Source, error) {
				return custom.LoadSource(path, true)
			},
		}
	}

	return providers
}

func Get(name string) (*Provider, bool) {
	customs := Customs()
	for _, provider := range customs {
		if provider.Name == name {
			return provider, true
		}
	}

	builtins := Builtins()
	for _, provider := range builtins {
		if provider.Name == name {
			return provider, true
		}
	}

	// Case-insensitive fallback to custom Lua sources (e.g. "Mangadex" -> "MangaDex.lua")
	for _, provider := range customs {
		if strings.EqualFold(provider.Name, name) {
			return provider, true
		}
	}

	for _, provider := range builtins {
		if strings.EqualFold(provider.Name, name) {
			return provider, true
		}
	}

	return nil, false
}
