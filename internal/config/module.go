package config

import (
	"context"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/configx"
)

var Module = func(options ...configx.Option) dix.Module {
	optCopy := append([]configx.Option(nil), options...)
	return dix.NewModule("config",
		dix.Providers(
			dix.ProviderErr0[Config](func() (Config, error) {
				return LoadWithOptions(context.Background(), optCopy...)
			}),
		),
	)
}
