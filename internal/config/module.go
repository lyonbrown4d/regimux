package config

import (
	"context"

	"github.com/arcgolabs/configx"
	"github.com/arcgolabs/dix"
	"github.com/samber/lo"
)

var Module = func(options ...configx.Option) dix.Module {
	optCopy := lo.Clone(options)
	return dix.NewModule("config",
		dix.Providers(
			dix.ProviderErr0[Config](func() (Config, error) {
				return LoadWithOptions(context.Background(), optCopy...)
			}),
		),
	)
}
