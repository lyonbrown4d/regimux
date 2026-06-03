// Package scheduler wires RegiMux background jobs.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

type PrefetchServiceDependencies struct {
	Metadata  meta.Store
	Tags      cache.TagService
	Manifests cache.ManifestService
	Blobs     cache.BlobService
	Logger    *slog.Logger
	Pools     *worker.Pools
}

type RuntimeDependencies struct {
	// runtime dependencies are assembled by dix.
	Config   config.Config
	Logger   *slog.Logger
	Cleanup  *cache.CleanupService
	Runtimes *collectionlist.List[ecosystem.Runtime]
	Metrics  *observability.Metrics
}

var Module = dix.NewModule("scheduler",
	dix.Providers(
		dix.Provider6[PrefetchServiceDependencies, meta.Store, cache.TagService, cache.ManifestService, cache.BlobService, *slog.Logger, *worker.Pools](
			newPrefetchServiceDependencies,
		),
		dix.Provider3[*ContainerRuntime, config.Config, *upstream.Client, *prefetch.Service](
			NewContainerRuntime,
			dix.Into[ecosystem.Runtime](dix.Key("container"), dix.Order(0)),
		),
		dix.Provider5[RuntimeDependencies, config.Config, *slog.Logger, *cache.CleanupService, *collectionlist.List[ecosystem.Runtime], *observability.Metrics](
			newRuntimeDependencies,
		),
		dix.Provider1[*prefetch.Service, PrefetchServiceDependencies](NewPrefetchService),
		dix.Provider1[*Runtime, RuntimeDependencies](NewRuntime),
	),
	dix.Hooks(
		dix.OnStart[*Runtime](startRuntime, dix.LifecycleName("regimux.scheduler_start"), dix.LifecyclePriority(50)),
		dix.OnStop[*Runtime](stopRuntime, dix.LifecycleName("regimux.scheduler_stop"), dix.LifecyclePriority(-50), dix.LifecycleTimeout(20*time.Second)),
	),
)

func NewPrefetchService(deps PrefetchServiceDependencies) *prefetch.Service {
	return prefetch.NewService(prefetch.ServiceDependencies{
		Metadata:  deps.Metadata,
		Tags:      deps.Tags,
		Manifests: deps.Manifests,
		Blobs:     deps.Blobs,
		Logger:    deps.Logger,
		Workers:   deps.Pools,
	})
}

func newPrefetchServiceDependencies(
	metadata meta.Store,
	tags cache.TagService,
	manifests cache.ManifestService,
	blobs cache.BlobService,
	logger *slog.Logger,
	pools *worker.Pools,
) PrefetchServiceDependencies {
	return PrefetchServiceDependencies{
		Metadata:  metadata,
		Tags:      tags,
		Manifests: manifests,
		Blobs:     blobs,
		Logger:    logger,
		Pools:     pools,
	}
}

func newRuntimeDependencies(
	cfg config.Config,
	logger *slog.Logger,
	cleanup *cache.CleanupService,
	runtimes *collectionlist.List[ecosystem.Runtime],
	metrics *observability.Metrics,
) RuntimeDependencies {
	return RuntimeDependencies{
		Config:   cfg,
		Logger:   logger,
		Cleanup:  cleanup,
		Runtimes: runtimes,
		Metrics:  metrics,
	}
}

func startRuntime(ctx context.Context, runtime *Runtime) error {
	if runtime == nil {
		return nil
	}
	return runtime.Start(ctx)
}

func stopRuntime(ctx context.Context, runtime *Runtime) error {
	if runtime == nil {
		return nil
	}
	return runtime.Stop(ctx)
}
