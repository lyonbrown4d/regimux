// Package scheduler wires RegiMux background jobs.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func Module(configModule, observabilityModule, cacheModule, storeModule dix.Module) dix.Module {
	return dix.NewModule("scheduler",
		dix.Imports(configModule, observabilityModule, cacheModule, storeModule),
		dix.Providers(
			dix.Provider6[*Runtime, config.Config, *slog.Logger, *cache.CleanupService, meta.Store, cache.TagService, cache.ManifestService](NewRuntime),
		),
		dix.Hooks(
			dix.OnStart[*Runtime](startRuntime, dix.LifecycleName("regimux.scheduler_start"), dix.LifecyclePriority(50)),
			dix.OnStop[*Runtime](stopRuntime, dix.LifecycleName("regimux.scheduler_stop"), dix.LifecyclePriority(-50), dix.LifecycleTimeout(20*time.Second)),
		),
	)
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
