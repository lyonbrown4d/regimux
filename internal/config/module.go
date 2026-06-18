package config

import (
	"context"

	"github.com/arcgolabs/configx"
	"github.com/arcgolabs/dix"
)

var Module = func(options ...configx.Option) dix.Module {
	optCopy := append([]configx.Option{}, options...)
	return dix.NewModule("config",
		dix.Providers(
			dix.ProviderErr0[Config](func() (Config, error) {
				return LoadWithOptions(context.Background(), optCopy...)
			}),
		),
	)
}
