package upstream

import (
	"log/slog"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func Module(configModule, observabilityModule dix.Module) dix.Module {
	return dix.NewModule("upstream",
		dix.Imports(configModule, observabilityModule),
		dix.Providers(
			dix.Provider2[*Client, config.Config, *slog.Logger](newClient, dix.As[RegistryClient]()),
		),
	)
}

func newClient(cfg config.Config, logger *slog.Logger) *Client {
	return NewClient(toUpstreamConfigs(cfg.OrderedUpstreams()), logger)
}

func toUpstreamConfigs(configs *collectionmapping.OrderedMap[string, config.UpstreamConfig]) *collectionmapping.OrderedMap[string, Config] {
	if configs == nil {
		return collectionmapping.NewOrderedMap[string, Config]()
	}
	out := collectionmapping.NewOrderedMapWithCapacity[string, Config](configs.Len())
	configs.Range(func(alias string, cfg config.UpstreamConfig) bool {
		out.Set(alias, toUpstreamConfig(alias, cfg))
		return true
	})
	return out
}

func toUpstreamConfig(alias string, cfg config.UpstreamConfig) Config {
	return Config{
		Alias:            alias,
		Registry:         cfg.Registry,
		Mirrors:          cfg.Mirrors,
		MirrorPolicy:     cfg.MirrorPolicy,
		DefaultNamespace: cfg.DefaultNamespace,
		TagTTL:           cfg.TagTTL.String(),
		Auth: AuthConfig{
			Type:     cfg.Auth.Type,
			Username: cfg.Auth.Username,
			Password: cfg.Auth.Password,
			Token:    cfg.Auth.Token,
		},
		HTTP: HTTPConfig{
			Timeout: cfg.HTTP.Timeout,
			Retry: HTTPRetryConfig{
				Enabled:    cfg.HTTP.Retry.Enabled,
				MaxRetries: cfg.HTTP.Retry.MaxRetries,
				WaitMin:    cfg.HTTP.Retry.WaitMin,
				WaitMax:    cfg.HTTP.Retry.WaitMax,
			},
			TLS: HTTPTLSConfig{
				Enabled:            cfg.HTTP.TLS.Enabled,
				InsecureSkipVerify: cfg.HTTP.TLS.InsecureSkipVerify,
				ServerName:         cfg.HTTP.TLS.ServerName,
			},
		},
	}
}
