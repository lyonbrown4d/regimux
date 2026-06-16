package cache

import (
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

var Module = dix.NewModule("container-cache",
	dix.Providers(
		dix.Provider6[*Proxy, upstream.RegistryClient, backend.Backend, meta.Store, object.Store, config.CacheConfig, events.Bus](
			func(client upstream.RegistryClient, cacheBackend backend.Backend, metadata meta.Store, objects object.Store, cacheCfg config.CacheConfig, bus events.Bus) *Proxy {
				return NewProxy(ProxyDependencies{
					Client:      client,
					Cache:       cacheBackend,
					Metadata:    metadata,
					Objects:     objects,
					CacheConfig: cacheCfg,
					Events:      bus,
				})
			},
		),
		dix.Provider2[*CleanupService, meta.Store, object.Store](NewCleanupService),
		dix.Provider1[ManifestService, *Proxy](func(proxy *Proxy) ManifestService {
			return proxy.Manifests()
		}),
		dix.Provider1[ManifestRefresher, *Proxy](func(proxy *Proxy) ManifestRefresher {
			refresher, ok := proxy.Manifests().(ManifestRefresher)
			if !ok {
				return nil
			}
			return refresher
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
