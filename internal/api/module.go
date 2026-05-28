package api

import (
	"context"
	"log/slog"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

type fiberOptions struct {
	routes []FiberRoute
	views  fiber.Views
}

var EndpointsModule = dix.NewModule("api-endpoints",
	dix.Providers(
		dix.Provider0[*HealthEndpoint](NewHealthEndpoint,
			dix.Into[httpx.Endpoint](dix.Key("health"), dix.Order(-100)),
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
		dix.Provider2[fiberOptions, *collectionlist.List[FiberRoute], *collectionlist.List[fiber.Views]](
			newFiberOptions,
		),
		dix.Provider6[*Server, config.ServerConfig, *slog.Logger, *collectionlist.List[httpx.Endpoint], fiberOptions, *observability.Metrics, *auth.Service](
			newServer,
			dix.Eager(),
		),
	),
	dix.Hooks(
		dix.OnStart[*Server](startServer, dix.LifecycleName("regimux.server_start"), dix.LifecyclePriority(0)),
		dix.OnStop[*Server](stopServer, dix.LifecycleName("regimux.server_stop"), dix.LifecyclePriority(0), dix.LifecycleTimeout(20*time.Second)),
	),
)

func newServer(
	cfg config.ServerConfig,
	logger *slog.Logger,
	endpoints *collectionlist.List[httpx.Endpoint],
	fiberOpts fiberOptions,
	metrics *observability.Metrics,
	authService *auth.Service,
) *Server {
	var values []httpx.Endpoint
	if endpoints != nil {
		values = endpoints.Values()
	}
	return NewServer(Options{
		Listen:       cfg.Listen,
		PublicURL:    cfg.PublicURL,
		Logger:       logger,
		Endpoints:    values,
		FiberRoutes:  fiberOpts.routes,
		Views:        fiberOpts.views,
		Metrics:      metrics,
		Auth:         authService,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
		PrintRoutes:  false,
	})
}

func newFiberOptions(routes *collectionlist.List[FiberRoute], views *collectionlist.List[fiber.Views]) fiberOptions {
	var opts fiberOptions
	if routes != nil {
		opts.routes = routes.Values()
	}
	if views != nil && views.Len() > 0 {
		opts.views = views.Values()[0]
	}
	return opts
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
