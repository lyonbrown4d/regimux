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
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
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
	storeModule := dix.NewModule("store",
		dix.Imports(configModule, observabilityModule),
		dix.Providers(
			dix.Provider2[meta.Store, config.Config, *slog.Logger](newMetadataStore, dix.Eager()),
			dix.Provider1[object.Store, config.Config](newObjectStore, dix.Eager()),
		),
	)
	cacheModule := dix.NewModule("cache",
		dix.Imports(configModule, observabilityModule, upstreamModule, storeModule),
		dix.Providers(
			dix.Provider2[backend.Backend, config.Config, *slog.Logger](newCacheBackend, dix.Eager()),
			dix.Provider5[*cache.Proxy, upstream.RegistryClient, backend.Backend, meta.Store, object.Store, config.Config](newCacheProxy),
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
		dix.Imports(configModule, observabilityModule, eventsModule, apiModule, cacheModule, storeModule),
		dix.Hooks(
			dix.OnStart2[config.Config, *slog.Logger](a.logStartup, dix.LifecycleName("regimux.log_startup"), dix.LifecyclePriority(-200)),
			dix.OnStart[events.Bus](a.publishStarting, dix.LifecycleName("regimux.application_starting"), dix.LifecyclePriority(-100)),
			dix.OnStart[Server](startServer, dix.LifecycleName("regimux.server_start"), dix.LifecyclePriority(0)),
			dix.OnStart[events.Bus](a.publishStarted, dix.LifecycleName("regimux.application_started"), dix.LifecyclePriority(100)),
			dix.OnStop[events.Bus](a.publishStopping, dix.LifecycleName("regimux.application_stopping"), dix.LifecyclePriority(100)),
			dix.OnStop[Server](stopServer, dix.LifecycleName("regimux.server_stop"), dix.LifecyclePriority(0), dix.LifecycleTimeout(20*time.Second)),
			dix.OnStop[events.Bus](a.publishStopped, dix.LifecycleName("regimux.application_stopped"), dix.LifecyclePriority(-100)),
			dix.OnStop[backend.Backend](closeCacheBackend, dix.LifecycleName("regimux.cache_close"), dix.LifecyclePriority(-150)),
			dix.OnStop[meta.Store](closeMetadataStore, dix.LifecycleName("regimux.meta_store_close"), dix.LifecyclePriority(-160)),
			dix.OnStop[events.Bus](closeBus, dix.LifecycleName("regimux.events_close"), dix.LifecyclePriority(-200)),
		),
	)

	return dix.New("regimuxd",
		dix.Version(a.version),
		dix.AppDescription("RegiMux registry proxy mirror gateway"),
		dix.UseLogger(a.logger),
		dix.RunStopTimeout(30*time.Second),
		dix.RecentEvents(128),
		dix.Modules(configModule, observabilityModule, eventsModule, upstreamModule, storeModule, cacheModule, endpointModule, apiModule, runtimeModule),
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
			HTTP: upstream.HTTPConfig{
				Timeout: cfg.HTTP.Timeout,
				Retry: upstream.HTTPRetryConfig{
					Enabled:    cfg.HTTP.Retry.Enabled,
					MaxRetries: cfg.HTTP.Retry.MaxRetries,
					WaitMin:    cfg.HTTP.Retry.WaitMin,
					WaitMax:    cfg.HTTP.Retry.WaitMax,
				},
				TLS: upstream.HTTPTLSConfig{
					Enabled:            cfg.HTTP.TLS.Enabled,
					InsecureSkipVerify: cfg.HTTP.TLS.InsecureSkipVerify,
					ServerName:         cfg.HTTP.TLS.ServerName,
				},
			},
		}
	}
	return out
}

func newCacheBackend(cfg config.Config, logger *slog.Logger) backend.Backend {
	switch cfg.Cache.Backend {
	case "redis":
		cache, err := backend.NewRedis(backend.KVOptions{
			Addrs:    cfg.Cache.Redis.Addrs,
			Username: cfg.Cache.Redis.Username,
			Password: cfg.Cache.Redis.Password,
			DB:       cfg.Cache.Redis.DB,
			Prefix:   cfg.Cache.Prefix,
			Logger:   logger,
			Debug:    cfg.Cache.Redis.Debug,
		})
		if err != nil {
			logger.Error("create redis cache backend failed", "error", err)
			return backend.Noop{}
		}
		return cache
	case "valkey":
		cache, err := backend.NewValkey(backend.KVOptions{
			Addrs:    cfg.Cache.Valkey.Addrs,
			Username: cfg.Cache.Valkey.Username,
			Password: cfg.Cache.Valkey.Password,
			DB:       cfg.Cache.Valkey.DB,
			Prefix:   cfg.Cache.Prefix,
			Logger:   logger,
			Debug:    cfg.Cache.Valkey.Debug,
		})
		if err != nil {
			logger.Error("create valkey cache backend failed", "error", err)
			return backend.Noop{}
		}
		return cache
	default:
		return backend.NewMemory(backend.MemoryOptions{
			MaxItems: cfg.Cache.Memory.MaxItems,
			Prefix:   cfg.Cache.Prefix,
		})
	}
}

func newMetadataStore(cfg config.Config, logger *slog.Logger) meta.Store {
	store, err := meta.OpenBbolt(cfg.Store.Meta.Path, logger)
	if err != nil {
		logger.Error("open metadata store failed", "path", cfg.Store.Meta.Path, "error", err)
		return nil
	}
	return store
}

func newObjectStore(cfg config.Config) object.Store {
	store, err := object.NewLocal(cfg.Store.Object.Path)
	if err != nil {
		return nil
	}
	return store
}

func newCacheProxy(client upstream.RegistryClient, cacheBackend backend.Backend, metadata meta.Store, objects object.Store, cfg config.Config) *cache.Proxy {
	return cache.NewProxy(
		client,
		cache.WithBackend(cacheBackend),
		cache.WithMetadata(metadata),
		cache.WithObjects(objects),
		cache.WithManifestTTL(cfg.Cache.Manifest.TagTTL),
		cache.WithTagsTTL(cfg.Cache.Tags.TTL),
		cache.WithReferrersTTL(cfg.Cache.Referrers.TTL),
		cache.WithReferrersFallbackTag(cfg.Cache.Referrers.FallbackTag),
	)
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

func closeCacheBackend(_ context.Context, cache backend.Backend) error {
	if cache == nil {
		return nil
	}
	return cache.Close()
}

func closeMetadataStore(_ context.Context, store meta.Store) error {
	if store == nil {
		return nil
	}
	return store.Close()
}
