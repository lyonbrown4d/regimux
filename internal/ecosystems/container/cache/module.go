package cache

import (
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("container-cache",
	dix.Providers(
		dix.Provider2[proxyStores, meta.Store, object.Store](newProxyStores),
		dix.Provider1[leaseRenewScheduler, *worker.Pools](func(pools *worker.Pools) leaseRenewScheduler {
			return pools.LeasePool()
		}),
		dix.Provider6[*Proxy, upstream.RegistryClient, backend.Backend, proxyStores, config.CacheConfig, events.Bus, leaseRenewScheduler](
			func(client upstream.RegistryClient, cacheBackend backend.Backend, stores proxyStores, cacheCfg config.CacheConfig, bus events.Bus, scheduler leaseRenewScheduler) *Proxy {
				return NewProxy(ProxyDependencies{
					Client:         client,
					Cache:          cacheBackend,
					Metadata:       stores.metadata,
					Objects:        stores.objects,
					CacheConfig:    cacheCfg,
					Events:         bus,
					LeaseScheduler: scheduler,
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

type proxyStores struct {
	metadata meta.Store
	objects  object.Store
}

func newProxyStores(metadata meta.Store, objects object.Store) proxyStores {
	return proxyStores{metadata: metadata, objects: objects}
}
