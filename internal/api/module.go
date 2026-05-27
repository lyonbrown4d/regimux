package api

import (
	"context"
	"log/slog"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

var EndpointsModule = dix.NewModule("api-endpoints",
	dix.Providers(
		dix.Provider0[*HealthEndpoint](NewHealthEndpoint,
			dix.Into[httpx.Endpoint](dix.Key("health"), dix.Order(-100)),
		),
		dix.Provider1[*MetricsEndpoint, *observability.Metrics](NewMetricsEndpoint,
			dix.Into[httpx.Endpoint](dix.Key("metrics"), dix.Order(-90)),
		),
		dix.Provider2[RegistryEndpointOptions, config.Config, *observability.Metrics](newRegistryEndpointOptions),
		dix.Provider6[*RegistryEndpoint, cache.ManifestService, cache.BlobService, cache.TagService, cache.ReferrerService, *slog.Logger, RegistryEndpointOptions](
			NewRegistryEndpointFromOptions,
			dix.Into[httpx.Endpoint](dix.Key("registry"), dix.Order(10)),
		),
	),
)

var Module = dix.NewModule("api",
	dix.Providers(
		dix.Provider1[config.ServerConfig, config.Config](func(cfg config.Config) config.ServerConfig {
			return cfg.Server
		}),
		dix.Provider3[*Server, config.ServerConfig, *slog.Logger, *collectionlist.List[httpx.Endpoint]](
			newServer,
			dix.Eager(),
		),
	),
	dix.Hooks(
		dix.OnStart[*Server](startServer, dix.LifecycleName("regimux.server_start"), dix.LifecyclePriority(0)),
		dix.OnStop[*Server](stopServer, dix.LifecycleName("regimux.server_stop"), dix.LifecyclePriority(0), dix.LifecycleTimeout(20*time.Second)),
	),
)

func newServer(cfg config.ServerConfig, logger *slog.Logger, endpoints *collectionlist.List[httpx.Endpoint]) *Server {
	var values []httpx.Endpoint
	if endpoints != nil {
		values = endpoints.Values()
	}
	return NewServer(Options{
		Listen:      cfg.Listen,
		PublicURL:   cfg.PublicURL,
		Logger:      logger,
		Endpoints:   values,
		PrintRoutes: false,
	})
}

func newRegistryEndpointOptions(cfg config.Config, metrics *observability.Metrics) RegistryEndpointOptions {
	return RegistryEndpointOptions{
		Config:  cfg,
		Metrics: metrics,
	}
}

func startServer(ctx context.Context, server *Server) error {
	if server == nil {
		return nil
	}
	return server.Start(ctx)
}

func stopServer(ctx context.Context, server *Server) error {
	if server == nil {
		return nil
	}
	return server.Stop(ctx)
}
