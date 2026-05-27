// Package worker wires shared worker pools.
package worker

import (
	"context"
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
)

var Module = dix.NewModule("worker",
	dix.Providers(
		dix.Provider1[config.WorkerConfig, config.Config](func(cfg config.Config) config.WorkerConfig {
			return cfg.Worker
		}),
		dix.Provider2[*Pools, config.WorkerConfig, *slog.Logger](NewPools),
	),
	dix.Hooks(
		dix.OnStop[*Pools](closePools, dix.LifecycleName("regimux.worker_stop"), dix.LifecyclePriority(-60)),
	),
)

func NewPools(cfg config.WorkerConfig, logger *slog.Logger) *Pools {
	if logger == nil {
		logger = slog.Default()
	}
	return NewPoolsConfig(cfg.ProbeConcurrency, cfg.PrefetchConcurrency, logger)
}

func closePools(_ context.Context, pools *Pools) error {
	if pools == nil {
		return nil
	}
	pools.Close()
	return nil
}
