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
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

type fiberOptions struct {
	routes *collectionlist.List[FiberRoute]
	views  fiber.Views
}

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
	return NewServer(Options{
		Listen:       cfg.Listen,
		PublicURL:    cfg.PublicURL,
		Logger:       logger,
		Endpoints:    endpoints,
		FiberRoutes:  fiberOpts.routes,
		Views:        fiberOpts.views,
		Metrics:      metrics,
		Auth:         authService,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
		Middleware:   cfg.Middleware,
		PrintRoutes:  false,
	})
}

func newFiberOptions(routes *collectionlist.List[FiberRoute], views *collectionlist.List[fiber.Views]) fiberOptions {
	opts := fiberOptions{
		routes: routes,
	}
	if views != nil && views.Len() > 0 {
		opts.views = views.Values()[0]
	}
	return opts
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
