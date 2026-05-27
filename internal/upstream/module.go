package upstream

import (
	"log/slog"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("upstream",
	dix.Providers(
		dix.Provider1[*Client, ClientDependencies](NewClient, dix.As[RegistryClient]()),
		dix.Provider4[ClientDependencies, config.Config, *slog.Logger, *worker.Pools, events.Bus](
			newClientDependencies,
		),
	),
)

func newClientDependencies(
	cfg config.Config,
	logger *slog.Logger,
	pools *worker.Pools,
	bus events.Bus,
) ClientDependencies {
	return ClientDependencies{
		Configs: toUpstreamConfigs(cfg.OrderedUpstreams()),
		Logger:  logger,
		Pools:   pools,
		Bus:     bus,
	}
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
		Blob: BlobConfig{
			MirrorPolicy:              cfg.Blob.MirrorPolicy,
			TopN:                      cfg.Blob.TopN,
			MaxConcurrencyPerEndpoint: cfg.Blob.MaxConcurrencyPerEndpoint,
		},
		Probe: ProbeConfig{
			Enabled:  cfg.Probe.Enabled,
			Interval: cfg.Probe.Interval,
			Timeout:  cfg.Probe.Timeout,
			Cooldown: cfg.Probe.Cooldown,
		},
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
