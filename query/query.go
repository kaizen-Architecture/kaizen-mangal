package query

import (
	"github.com/metafates/gache"
	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/where"
)

type queryRecord struct {
	Rank  int    `json:"rank"`
	Query string `json:"query"`
}

var cacher = gache.New[map[string]*queryRecord](
	&gache.Options{
		Path:       where.Queries(),
		FileSystem: &filesystem.GacheFs{},
	},
)
