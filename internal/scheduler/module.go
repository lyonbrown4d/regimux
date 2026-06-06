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
	"github.com/lyonbrown4d/regimux/internal/observability"
)

type RuntimeDependencies struct {
	// runtime dependencies are assembled by dix.
	Config   config.Config
	Logger   *slog.Logger
	Runtimes *collectionlist.List[ecosystem.Runtime]
	Metrics  *observability.Metrics
}

var Module = dix.NewModule("scheduler",
	dix.Providers(
		dix.Provider4[RuntimeDependencies, config.Config, *slog.Logger, *collectionlist.List[ecosystem.Runtime], *observability.Metrics](
			newRuntimeDependencies,
		),
		dix.Provider1[*Runtime, RuntimeDependencies](NewRuntime),
	),
	dix.Hooks(
		dix.OnStart[*Runtime](startRuntime, dix.LifecycleName("regimux.scheduler_start"), dix.LifecyclePriority(50)),
		dix.OnStop[*Runtime](stopRuntime, dix.LifecycleName("regimux.scheduler_stop"), dix.LifecyclePriority(-50), dix.LifecycleTimeout(20*time.Second)),
	),
)

func newRuntimeDependencies(
	cfg config.Config,
	logger *slog.Logger,
	runtimes *collectionlist.List[ecosystem.Runtime],
	metrics *observability.Metrics,
) RuntimeDependencies {
	return RuntimeDependencies{
		Config:   cfg,
		Logger:   logger,
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
