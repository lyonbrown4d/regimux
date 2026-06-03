package upstream

import (
	"context"
	"log/slog"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/probehealth"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("upstream",
	dix.Providers(
		dix.Provider2[*EndpointClients, config.Config, *slog.Logger](newEndpointClients),
		dix.Provider1[*Client, ClientDependencies](NewClient, dix.As[RegistryClient]()),
		dix.Provider2[clientRuntimeDependencies, *EndpointClients, probehealth.Store](newClientRuntimeDependencies),
		dix.Provider6[ClientDependencies, config.Config, *slog.Logger, *worker.Pools, events.Bus, meta.Store, clientRuntimeDependencies](
			newClientDependencies,
		),
	),
	dix.Hooks(
		dix.OnStart[*Client](loadClientEndpointHealth, dix.LifecycleName("regimux.upstream_health_load"), dix.LifecyclePriority(-50)),
		dix.OnStop[*Client](flushClientEndpointHealth, dix.LifecycleName("regimux.upstream_health_flush"), dix.LifecyclePriority(-55)),
		dix.OnStop[*EndpointClients](closeEndpointClients, dix.LifecycleName("regimux.upstream_endpoint_clients_close"), dix.LifecyclePriority(-60)),
	),
)

func newEndpointClients(cfg config.Config, logger *slog.Logger) *EndpointClients {
	return newEndpointClientsFromConfigs(ConfigsFromUpstreamConfigs(cfg.OrderedContainerUpstreams()), logger)
}

func newClientDependencies(
	cfg config.Config,
	logger *slog.Logger,
	pools *worker.Pools,
	bus events.Bus,
	metadata meta.Store,
	runtime clientRuntimeDependencies,
) ClientDependencies {
	return ClientDependencies{
		Configs:         ConfigsFromUpstreamConfigs(cfg.OrderedContainerUpstreams()),
		Logger:          logger,
		Pools:           pools,
		Bus:             bus,
		Metadata:        metadata,
		EndpointClients: runtime.EndpointClients,
		HotHealth:       runtime.HotHealth,
	}
}

type clientRuntimeDependencies struct {
	EndpointClients *EndpointClients
	HotHealth       probehealth.Store
}

func newClientRuntimeDependencies(endpointClients *EndpointClients, hotHealth probehealth.Store) clientRuntimeDependencies {
	return clientRuntimeDependencies{
		EndpointClients: endpointClients,
		HotHealth:       hotHealth,
	}
}

func loadClientEndpointHealth(ctx context.Context, client *Client) error {
	if client == nil {
		return nil
	}
	return client.LoadEndpointHealth(ctx)
}

func flushClientEndpointHealth(ctx context.Context, client *Client) error {
	if client == nil {
		return nil
	}
	return client.FlushEndpointHealth(ctx)
}

func closeEndpointClients(_ context.Context, clients *EndpointClients) error {
	if clients == nil {
		return nil
	}
	return clients.Close()
}

// ConfigsFromUpstreamConfigs converts runtime config upstreams into client configs.
func ConfigsFromUpstreamConfigs(configs *collectionmapping.OrderedMap[string, config.UpstreamConfig]) *collectionmapping.OrderedMap[string, Config] {
	if configs == nil {
		return collectionmapping.NewOrderedMap[string, Config]()
	}
	out := collectionmapping.NewOrderedMapWithCapacity[string, Config](configs.Len())
	configs.Range(func(alias string, cfg config.UpstreamConfig) bool {
		if cfg.Type != "" && cfg.Type != "oci" {
			return true
		}
		out.Set(alias, ConfigFromUpstreamConfig(alias, cfg))
		return true
	})
	return out
}

// ConfigFromUpstreamConfig converts one runtime upstream config into a client config.
func ConfigFromUpstreamConfig(alias string, cfg config.UpstreamConfig) Config {
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
			MaxConcurrentAttempts:     cfg.Blob.MaxConcurrentAttempts,
		},
		Probe: ProbeConfig{
			Enabled:  cfg.Probe.Enabled,
			Interval: cfg.Probe.Interval,
			Timeout:  cfg.Probe.Timeout,
			Cooldown: cfg.Probe.Cooldown,
			Jitter:   cfg.Probe.Jitter,
		},
		Auth: AuthConfig{
			Type:     cfg.Auth.Type,
			Username: cfg.Auth.Username,
			Password: cfg.Auth.Password,
			Token:    cfg.Auth.Token,
		},
		HTTP: HTTPConfig{
			Timeout: cfg.HTTP.Timeout,
			HTTP2: HTTP2Config{
				Enabled: cfg.HTTP.HTTP2.Enabled,
			},
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
