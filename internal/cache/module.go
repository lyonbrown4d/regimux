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
		dix.ProviderErr2[backend.Backend, config.Config, *slog.Logger](newBackend, dix.Eager()),
		dix.Provider6[*Proxy, upstream.RegistryClient, backend.Backend, meta.Store, object.Store, config.Config, events.Bus](newProxy),
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

func newBackend(cfg config.Config, logger *slog.Logger) (backend.Backend, error) {
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
			return nil, oops.Wrapf(err, "create redis cache backend")
		}
		return cache, nil
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
			return nil, oops.Wrapf(err, "create valkey cache backend")
		}
		return cache, nil
	default:
		return backend.NewMemory(backend.MemoryOptions{
			MaxItems: cfg.Cache.Memory.MaxItems,
			Prefix:   cfg.Cache.Prefix,
		}), nil
	}
}

func newProxy(client upstream.RegistryClient, cacheBackend backend.Backend, metadata meta.Store, objects object.Store, cfg config.Config, bus events.Bus) *Proxy {
	return NewProxy(
		client,
		WithBackend(cacheBackend),
		WithMetadata(metadata),
		WithObjects(objects),
		WithEvents(bus),
		WithManifestTTL(cfg.Cache.Manifest.TagTTL),
		WithManifestStaleIfError(cfg.Cache.Manifest.StaleIfError),
		WithManifestMaxStale(cfg.Cache.Manifest.MaxStale),
		WithTagsTTL(cfg.Cache.Tags.TTL),
		WithReferrersTTL(cfg.Cache.Referrers.TTL),
		WithReferrersFallbackTag(cfg.Cache.Referrers.FallbackTag),
	)
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
