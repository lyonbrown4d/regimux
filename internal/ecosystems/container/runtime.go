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
	"github.com/samber/oops"
)

type Runtime struct {
	cfg      config.Config
	upstream *upstream.Client
	prefetch *prefetch.Service
	cleanup  *cache.CleanupService
	manual   *manualsync.Service
}

func NewRuntime(cfg config.Config, upstreamClient *upstream.Client, prefetchService *prefetch.Service, cleanupService *cache.CleanupService) *Runtime {
	manual := manualsync.NewService(manualsync.ServiceDependencies{
		Execute: func(ctx context.Context, opts manualsync.SyncOptions) (*manualsync.SyncReport, error) {
			if prefetchService == nil {
				return nil, oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container manual sync service is not configured")
			}
			return prefetchService.Sync(ctx, opts)
		},
	})
	return &Runtime{
		cfg:      cfg,
		upstream: upstreamClient,
		prefetch: prefetchService,
		cleanup:  cleanupService,
		manual:   manual,
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
	out := collectionlist.NewList[ecosystem.Upstream]()
	ordered.Range(func(alias string, cfg config.UpstreamConfig) bool {
		out.Add(ecosystem.Upstream{
			Ecosystem: r.Name(),
			Alias:     alias,
			Config:    cfg,
		})
		return true
	})
	return out
}

var _ ecosystem.Runtime = (*Runtime)(nil)
var _ ecosystem.UpstreamProvider = (*Runtime)(nil)
var _ ecosystem.Prober = (*Runtime)(nil)
var _ ecosystem.Cleaner = (*Runtime)(nil)
var _ ecosystem.Prefetcher = (*Runtime)(nil)
var _ ecosystem.UpstreamSnapshotProvider = (*Runtime)(nil)
var _ ecosystem.PrefetchController = (*Runtime)(nil)
var _ ecosystem.EndpointHealthFlusher = (*Runtime)(nil)
var _ ecosystem.JobProvider = (*Runtime)(nil)
