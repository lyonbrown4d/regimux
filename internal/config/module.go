package config

import (
	"context"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/configx"
)

func Module(cfg Config) dix.Module {
	return dix.NewModule("config",
		dix.Providers(
			dix.Value(cfg),
		),
	)
}

func ModuleFromOptions(options ...configx.Option) dix.Module {
	optCopy := append([]configx.Option(nil), options...)
	return dix.NewModule("config",
		dix.Providers(
			dix.ProviderErr0[Config](func() (Config, error) {
				return LoadWithOptions(context.Background(), optCopy...)
			}),
		),
	)
}
