// Package scheduler wires RegiMux background jobs.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

type RuntimeDependencies struct {
	// runtime dependencies are assembled by dix.
	Config   config.Config
	Logger   *slog.Logger
	Runtimes *collectionlist.List[ecosystem.Runtime]
	Metrics  *observability.Metrics
	Metadata meta.Store
	Objects  object.Store
	Workers  *worker.Pools
}

var Module = dix.NewModule("scheduler",
	dix.Providers(
		dix.Provider2[cleanupStores, meta.Store, object.Store](newCleanupStores),
		dix.Provider6[*Runtime, config.Config, *slog.Logger, *collectionlist.List[ecosystem.Runtime], *observability.Metrics, cleanupStores, *worker.Pools](
			func(cfg config.Config, logger *slog.Logger, runtimes *collectionlist.List[ecosystem.Runtime], metrics *observability.Metrics, stores cleanupStores, workers *worker.Pools) *Runtime {
				return NewRuntime(RuntimeDependencies{
					Config:   cfg,
					Logger:   logger,
					Runtimes: runtimes,
					Metrics:  metrics,
					Metadata: stores.metadata,
					Objects:  stores.objects,
					Workers:  workers,
				})
			},
		),
		dix.Contribute1[events.Subscriber, *Runtime](
			NewRefreshSubscriber,
			dix.Key("scheduler.refresh"), dix.Order(50),
		),
	),
	dix.Hooks(
		dix.OnStart[*Runtime](startRuntime, dix.LifecycleName("regimux.scheduler_start"), dix.LifecyclePriority(50)),
		dix.OnStop[*Runtime](stopRuntime, dix.LifecycleName("regimux.scheduler_stop"), dix.LifecyclePriority(-50), dix.LifecycleTimeout(20*time.Second)),
	),
)

type cleanupStores struct {
	metadata meta.Store
	objects  object.Store
}

func newCleanupStores(metadata meta.Store, objects object.Store) cleanupStores {
	return cleanupStores{metadata: metadata, objects: objects}
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
