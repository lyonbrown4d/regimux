package artifactcache

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("artifact-cache",
	dix.Providers(
		dix.Provider3[*FillTracker, backend.Backend, *worker.Pools, *slog.Logger](newFillTracker),
		dix.Provider4[*Store, meta.Store, object.Store, *FillTracker, *slog.Logger](
			func(metadata meta.Store, objects object.Store, fills *FillTracker, logger *slog.Logger) *Store {
				return New(Dependencies{
					Metadata: metadata,
					Objects:  objects,
					Fills:    fills,
					Logger:   logger,
				})
			},
		),
	),
)

func newFillTracker(cacheBackend backend.Backend, pools *worker.Pools, logger *slog.Logger) *FillTracker {
	locker, ok := cacheBackend.(FillLocker)
	if !ok {
		locker = nil
	}
	return NewFillTracker(
		WithFillLocker(locker),
		WithFillLeaseScheduler(pools.LeasePool()),
		WithFillLogger(componentLogger(logger, "artifact-cache")),
	)
}

func componentLogger(logger *slog.Logger, component string) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With("component", component)
}
