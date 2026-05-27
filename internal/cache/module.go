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
		dix.OnStop[backend.Backend](
			closeBackend,
			dix.LifecycleName("regimux.cache_close"),
			dix.LifecyclePriority(-150),
		),
	),
)

func newBackend(cfg config.CacheConfig, logger *slog.Logger) (backend.Backend, error) {
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
		return cache, nil
	default:
		return backend.NewMemory(backend.MemoryOptions{
			MaxItems: cfg.Memory.MaxItems,
			Prefix:   cfg.Prefix,
		}), nil
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

func closeBackend(_ context.Context, backend backend.Backend) error {
	if backend == nil {
		return nil
	}
	if err := backend.Close(); err != nil {
		return oops.Wrapf(err, "close cache backend")
	}
	return nil
}
