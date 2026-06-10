package container

import (
	"context"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/prefetch"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/samber/lo"
	"github.com/samber/oops"
)

type Runtime struct {
	cfg             config.Config
	upstream        *upstream.Client
	prefetch        *prefetch.Service
	manifestRefresh cache.ManifestRefresher
	cleanup         *cache.CleanupService
	manual          *manualsync.Service
}

func NewRuntime(
	cfg config.Config,
	upstreamClient *upstream.Client,
	prefetchService *prefetch.Service,
	manifests cache.ManifestService,
	cleanupService *cache.CleanupService,
) *Runtime {
	var manifestRefresher cache.ManifestRefresher
	if refresher, ok := manifests.(cache.ManifestRefresher); ok {
		manifestRefresher = refresher
	}
	manual := manualsync.NewService(manualsync.ServiceDependencies{
		Execute: func(ctx context.Context, opts manualsync.SyncOptions) (*manualsync.SyncReport, error) {
			if prefetchService == nil {
				return nil, oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container manual sync service is not configured")
			}
			return prefetchService.Sync(ctx, opts)
		},
	})
	return &Runtime{
		cfg:             cfg,
		upstream:        upstreamClient,
		prefetch:        prefetchService,
		manifestRefresh: manifestRefresher,
		cleanup:         cleanupService,
		manual:          manual,
	}
}

func (r *Runtime) Name() string {
	return ecosystem.Container
}

func (r *Runtime) Upstreams() *collectionlist.List[ecosystem.Upstream] {
	if r == nil {
		return collectionlist.NewList[ecosystem.Upstream]()
	}
	ordered := r.cfg.OrderedContainerUpstreams()
	return collectionlist.NewList(lo.Map(ordered.Keys(), func(alias string, _ int) ecosystem.Upstream {
		cfg, _ := ordered.Get(alias)
		return ecosystem.Upstream{
			Ecosystem: r.Name(),
			Alias:     alias,
			Config:    cfg,
		}
	})...)
}

var _ ecosystem.Runtime = (*Runtime)(nil)
var _ ecosystem.UpstreamProvider = (*Runtime)(nil)
var _ ecosystem.Prober = (*Runtime)(nil)
var _ ecosystem.Cleaner = (*Runtime)(nil)
var _ ecosystem.Prefetcher = (*Runtime)(nil)
var _ ecosystem.Refresher = (*Runtime)(nil)
var _ ecosystem.UpstreamSnapshotProvider = (*Runtime)(nil)
var _ ecosystem.PrefetchController = (*Runtime)(nil)
var _ ecosystem.EndpointHealthFlusher = (*Runtime)(nil)
var _ ecosystem.JobProvider = (*Runtime)(nil)
var _ ecosystem.ManualSyncer = (*Runtime)(nil)
