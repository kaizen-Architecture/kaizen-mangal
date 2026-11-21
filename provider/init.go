package provider

import (
	"github.com/kaizen-architecture/kaizen-mangal/provider/generic"
	"github.com/kaizen-architecture/kaizen-mangal/provider/mangadex"
	"github.com/kaizen-architecture/kaizen-mangal/provider/manganato"
	"github.com/kaizen-architecture/kaizen-mangal/provider/manganelo"
	"github.com/kaizen-architecture/kaizen-mangal/provider/mangapill"
	"github.com/kaizen-architecture/kaizen-mangal/source"
)

const CustomProviderExtension = ".lua"

var builtinProviders = []*Provider{
	{
		ID:   mangadex.ID,
		Name: mangadex.Name,
		CreateSource: func() (source.Source, error) {
			return mangadex.New(), nil
		},
	},
}

func init() {
	for _, conf := range []*generic.Configuration{
		manganelo.Config,
		manganato.Config,
		mangapill.Config,
	} {
		conf := conf
		builtinProviders = append(builtinProviders, &Provider{
			ID:   conf.ID(),
			Name: conf.Name,
			CreateSource: func() (source.Source, error) {
				return generic.New(conf), nil
			},
		})
	}
}
