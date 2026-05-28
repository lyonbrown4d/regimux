package cache

import (
	"context"
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/samber/oops"
)

var Module = dix.NewModule("cache",
	dix.Providers(
		dix.Provider1[config.CacheConfig, config.Config](func(cfg config.Config) config.CacheConfig {
			return cfg.Cache
		}),
		dix.ProviderErr2[backend.Backend, config.CacheConfig, *slog.Logger](newBackend, dix.Eager()),
		dix.Provider6[ProxyDependencies, upstream.RegistryClient, backend.Backend, meta.Store, object.Store, config.CacheConfig, events.Bus](newProxyDependencies),
		dix.Provider1[*Proxy, ProxyDependencies](NewProxy),
		dix.Provider2[*CleanupService, meta.Store, object.Store](NewCleanupService),
		dix.Provider1[ManifestService, *Proxy](func(proxy *Proxy) ManifestService {
			return proxy.Manifests()
		}),
		dix.Provider1[BlobService, *Proxy](func(proxy *Proxy) BlobService {
			return proxy.Blobs()
		}),
		dix.Provider1[TagService, *Proxy](func(proxy *Proxy) TagService {
			return proxy.Tags()
		}),
		dix.Provider1[ReferrerService, *Proxy](func(proxy *Proxy) ReferrerService {
			return proxy.Referrers()
		}),
	),
	dix.Hooks(
		dix.OnStop2[backend.Backend, *slog.Logger](
			closeBackend,
			dix.LifecycleName("regimux.cache_close"),
			dix.LifecyclePriority(-150),
		),
	),
)

func newBackend(cfg config.CacheConfig, logger *slog.Logger) (backend.Backend, error) {
	logger = componentLogger(logger, "cache")
	logger.Info("opening cache backend", "backend", cfg.Backend, "prefix", cfg.Prefix)
	switch cfg.Backend {
	case "redis":
		cache, err := backend.NewRedis(backend.KVOptions{
			Addrs:    cfg.Redis.Addrs,
			Username: cfg.Redis.Username,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
			Prefix:   cfg.Prefix,
			Logger:   logger,
			Debug:    cfg.Redis.Debug,
		})
		if err != nil {
			return nil, oops.Wrapf(err, "create redis cache backend")
		}
		logger.Info("cache backend opened", "backend", "redis", "addrs", cfg.Redis.Addrs)
		return cache, nil
	case "valkey":
		cache, err := backend.NewValkey(backend.KVOptions{
			Addrs:    cfg.Valkey.Addrs,
			Username: cfg.Valkey.Username,
			Password: cfg.Valkey.Password,
			DB:       cfg.Valkey.DB,
			Prefix:   cfg.Prefix,
			Logger:   logger,
			Debug:    cfg.Valkey.Debug,
		})
		if err != nil {
			return nil, oops.Wrapf(err, "create valkey cache backend")
		}
		logger.Info("cache backend opened", "backend", "valkey", "addrs", cfg.Valkey.Addrs)
		return cache, nil
	default:
		cache := backend.NewMemory(backend.MemoryOptions{
			MaxItems: cfg.Memory.MaxItems,
			Prefix:   cfg.Prefix,
		})
		logger.Info("cache backend opened", "backend", "memory", "max_items", cfg.Memory.MaxItems)
		return cache, nil
	}
}

func newProxyDependencies(
	client upstream.RegistryClient,
	cacheBackend backend.Backend,
	metadata meta.Store,
	objects object.Store,
	cacheCfg config.CacheConfig,
	bus events.Bus,
) ProxyDependencies {
	return ProxyDependencies{
		Client:      client,
		Cache:       cacheBackend,
		Metadata:    metadata,
		Objects:     objects,
		CacheConfig: cacheCfg,
		Events:      bus,
	}
}

func closeBackend(_ context.Context, cacheBackend backend.Backend, logger *slog.Logger) error {
	if cacheBackend == nil {
		return nil
	}
	logger = componentLogger(logger, "cache")
	logger.Info("closing cache backend")
	if err := cacheBackend.Close(); err != nil {
		return oops.Wrapf(err, "close cache backend")
	}
	logger.Info("cache backend closed")
	return nil
}

func componentLogger(logger *slog.Logger, component string) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With("component", component)
}
