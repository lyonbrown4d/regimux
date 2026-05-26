package config

import (
	"context"

	"github.com/arcgolabs/dix"
)

func Module(cfg Config) dix.Module {
	return dix.NewModule("config",
		dix.Providers(
			dix.Value(cfg),
		),
	)
}

func ModuleFromPath(configPath string, args ...string) dix.Module {
	configArgs := append([]string(nil), args...)
	return dix.NewModule("config",
		dix.Providers(
			dix.ProviderErr0[Config](func() (Config, error) {
				return Load(context.Background(), configPath, configArgs...)
			}),
		),
	)
}
