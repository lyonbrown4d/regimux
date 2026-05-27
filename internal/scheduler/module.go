// Package scheduler wires RegiMux background jobs.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
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
	Logger    *slog.Logger
	Pools     *worker.Pools
}

type RuntimeDependencies struct {
	Config   config.Config
	Logger   *slog.Logger
	Cleanup  *cache.CleanupService
	Prefetch *prefetch.Service
	Upstream *upstream.Client
	Metrics  *observability.Metrics
}

var Module = dix.NewModule("scheduler",
	dix.Providers(
		dix.Provider5[PrefetchServiceDependencies, meta.Store, cache.TagService, cache.ManifestService, *slog.Logger, *worker.Pools](
			newPrefetchServiceDependencies,
		),
		dix.Provider6[RuntimeDependencies, config.Config, *slog.Logger, *cache.CleanupService, *prefetch.Service, *upstream.Client, *observability.Metrics](
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
		Logger:    deps.Logger,
		Workers:   deps.Pools,
	})
}

func newPrefetchServiceDependencies(
	metadata meta.Store,
	tags cache.TagService,
	manifests cache.ManifestService,
	logger *slog.Logger,
	pools *worker.Pools,
) PrefetchServiceDependencies {
	return PrefetchServiceDependencies{
		Metadata:  metadata,
		Tags:      tags,
		Manifests: manifests,
		Logger:    logger,
		Pools:     pools,
	}
}

func newRuntimeDependencies(
	cfg config.Config,
	logger *slog.Logger,
	cleanup *cache.CleanupService,
	prefetchService *prefetch.Service,
	upstreamClient *upstream.Client,
	metrics *observability.Metrics,
) RuntimeDependencies {
	return RuntimeDependencies{
		Config:   cfg,
		Logger:   logger,
		Cleanup:  cleanup,
		Prefetch: prefetchService,
		Upstream: upstreamClient,
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
