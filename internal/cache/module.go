package cache

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func Module(configModule, observabilityModule, upstreamModule, storeModule dix.Module) dix.Module {
	return dix.NewModule("cache",
		dix.Imports(configModule, observabilityModule, upstreamModule, storeModule),
		dix.Providers(
			dix.Provider2[backend.Backend, config.Config, *slog.Logger](newBackend, dix.Eager()),
			dix.Provider5[*Proxy, upstream.RegistryClient, backend.Backend, meta.Store, object.Store, config.Config](newProxy),
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
	)
}

func newBackend(cfg config.Config, logger *slog.Logger) backend.Backend {
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

func newProxy(client upstream.RegistryClient, cacheBackend backend.Backend, metadata meta.Store, objects object.Store, cfg config.Config) *Proxy {
	return NewProxy(
		client,
		WithBackend(cacheBackend),
		WithMetadata(metadata),
		WithObjects(objects),
		WithManifestTTL(cfg.Cache.Manifest.TagTTL),
		WithTagsTTL(cfg.Cache.Tags.TTL),
		WithReferrersTTL(cfg.Cache.Referrers.TTL),
		WithReferrersFallbackTag(cfg.Cache.Referrers.FallbackTag),
	)
}
