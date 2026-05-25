package app

import (
	"context"
	"log/slog"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

type Server interface {
	Start(context.Context) error
	Stop(context.Context) error
}

type Application struct {
	cfg     config.Config
	logger  *slog.Logger
	version string
}

func New(cfg config.Config, logger *slog.Logger, version string) *Application {
	if logger == nil {
		logger = slog.Default()
	}
	return &Application{
		cfg:     cfg,
		logger:  logger,
		version: version,
	}
}

func (a *Application) Run() error {
	return a.build().Run()
}

func (a *Application) RunContext(ctx context.Context) error {
	return a.build().RunContext(ctx)
}

func (a *Application) build() *dix.App {
	configModule := dix.NewModule("config",
		dix.Providers(
			dix.Value(a.cfg),
		),
	)
	observabilityModule := dix.NewModule("observability",
		dix.Providers(
			dix.Value(a.logger),
		),
	)
	eventsModule := dix.NewModule("events",
		dix.Imports(observabilityModule),
		dix.Providers(
			dix.Provider1[events.Bus, *slog.Logger](events.NewBus, dix.Eager()),
		),
	)
	upstreamModule := dix.NewModule("upstream",
		dix.Imports(configModule, observabilityModule),
		dix.Providers(
			dix.Provider2[*upstream.Client, config.Config, *slog.Logger](newUpstreamClient, dix.As[upstream.RegistryClient]()),
		),
	)
	cacheModule := dix.NewModule("cache",
		dix.Imports(upstreamModule),
		dix.Providers(
			dix.Provider1[*cache.Proxy, upstream.RegistryClient](cache.NewProxy),
			dix.Provider1[cache.ManifestService, *cache.Proxy](func(proxy *cache.Proxy) cache.ManifestService {
				return proxy.Manifests()
			}),
			dix.Provider1[cache.BlobService, *cache.Proxy](func(proxy *cache.Proxy) cache.BlobService {
				return proxy.Blobs()
			}),
			dix.Provider1[cache.TagService, *cache.Proxy](func(proxy *cache.Proxy) cache.TagService {
				return proxy.Tags()
			}),
			dix.Provider1[cache.ReferrerService, *cache.Proxy](func(proxy *cache.Proxy) cache.ReferrerService {
				return proxy.Referrers()
			}),
		),
	)
	endpointModule := dix.NewModule("api-endpoints",
		dix.Imports(cacheModule, observabilityModule),
		dix.Providers(
			dix.Provider0[*api.HealthEndpoint](api.NewHealthEndpoint,
				dix.Into[httpx.Endpoint](dix.Key("health"), dix.Order(-100)),
			),
			dix.Provider5[*api.RegistryEndpoint, cache.ManifestService, cache.BlobService, cache.TagService, cache.ReferrerService, *slog.Logger](
				api.NewRegistryEndpoint,
				dix.Into[httpx.Endpoint](dix.Key("registry"), dix.Order(10)),
			),
		),
	)
	apiModule := dix.NewModule("api",
		dix.Imports(configModule, observabilityModule, eventsModule, endpointModule),
		dix.Providers(
			dix.Provider4[*api.Server, config.Config, *slog.Logger, events.Bus, *collectionlist.List[httpx.Endpoint]](
				newAPIServer,
				dix.As[Server](),
				dix.Eager(),
			),
		),
	)
	runtimeModule := dix.NewModule("runtime",
		dix.Imports(configModule, observabilityModule, eventsModule, apiModule),
		dix.Hooks(
			dix.OnStart2[config.Config, *slog.Logger](a.logStartup, dix.LifecycleName("regimux.log_startup"), dix.LifecyclePriority(-200)),
			dix.OnStart[events.Bus](a.publishStarting, dix.LifecycleName("regimux.application_starting"), dix.LifecyclePriority(-100)),
			dix.OnStart[Server](startServer, dix.LifecycleName("regimux.server_start"), dix.LifecyclePriority(0)),
			dix.OnStart[events.Bus](a.publishStarted, dix.LifecycleName("regimux.application_started"), dix.LifecyclePriority(100)),
			dix.OnStop[events.Bus](a.publishStopping, dix.LifecycleName("regimux.application_stopping"), dix.LifecyclePriority(100)),
			dix.OnStop[Server](stopServer, dix.LifecycleName("regimux.server_stop"), dix.LifecyclePriority(0), dix.LifecycleTimeout(20*time.Second)),
			dix.OnStop[events.Bus](a.publishStopped, dix.LifecycleName("regimux.application_stopped"), dix.LifecyclePriority(-100)),
			dix.OnStop[events.Bus](closeBus, dix.LifecycleName("regimux.events_close"), dix.LifecyclePriority(-200)),
		),
	)

	return dix.New("regimuxd",
		dix.Version(a.version),
		dix.AppDescription("RegiMux registry proxy mirror gateway"),
		dix.UseLogger(a.logger),
		dix.RunStopTimeout(30*time.Second),
		dix.RecentEvents(128),
		dix.Modules(configModule, observabilityModule, eventsModule, upstreamModule, cacheModule, endpointModule, apiModule, runtimeModule),
	)
}

func newUpstreamClient(cfg config.Config, logger *slog.Logger) *upstream.Client {
	return upstream.NewClient(toUpstreamConfigs(cfg.Upstreams), logger)
}

func toUpstreamConfigs(configs map[string]config.UpstreamConfig) map[string]upstream.Config {
	out := make(map[string]upstream.Config, len(configs))
	for alias, cfg := range configs {
		out[alias] = upstream.Config{
			Alias:            alias,
			Registry:         cfg.Registry,
			DefaultNamespace: cfg.DefaultNamespace,
			TagTTL:           cfg.TagTTL.String(),
			Auth: upstream.AuthConfig{
				Type:     cfg.Auth.Type,
				Username: cfg.Auth.Username,
				Password: cfg.Auth.Password,
				Token:    cfg.Auth.Token,
			},
		}
	}
	return out
}

func newAPIServer(cfg config.Config, logger *slog.Logger, _ events.Bus, endpoints *collectionlist.List[httpx.Endpoint]) *api.Server {
	var values []httpx.Endpoint
	if endpoints != nil {
		values = endpoints.Values()
	}
	return api.NewServer(api.Options{
		Listen:      cfg.Server.Listen,
		PublicURL:   cfg.Server.PublicURL,
		Logger:      logger,
		Endpoints:   values,
		PrintRoutes: false,
	})
}

func (a *Application) logStartup(_ context.Context, cfg config.Config, logger *slog.Logger) error {
	ordered := cfg.OrderedUpstreams()
	logger.Info("regimuxd starting",
		"version", a.version,
		"listen", cfg.Server.Listen,
		"upstream_count", ordered.Len(),
		"upstreams", ordered.Keys(),
	)
	return nil
}

func (a *Application) publishStarting(ctx context.Context, bus events.Bus) error {
	return events.Publish(ctx, bus, events.ApplicationStarting{Version: a.version})
}

func (a *Application) publishStarted(ctx context.Context, bus events.Bus) error {
	return events.Publish(ctx, bus, events.ApplicationStarted{Version: a.version})
}

func (a *Application) publishStopping(ctx context.Context, bus events.Bus) error {
	return events.Publish(ctx, bus, events.ApplicationStopping{Version: a.version})
}

func (a *Application) publishStopped(ctx context.Context, bus events.Bus) error {
	return events.Publish(ctx, bus, events.ApplicationStopped{Version: a.version})
}

func startServer(ctx context.Context, server Server) error {
	if server == nil {
		return nil
	}
	return server.Start(ctx)
}

func stopServer(ctx context.Context, server Server) error {
	if server == nil {
		return nil
	}
	return server.Stop(ctx)
}

func closeBus(_ context.Context, bus events.Bus) error {
	if bus == nil {
		return nil
	}
	return bus.Close()
}
