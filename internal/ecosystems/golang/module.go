// Package golang exposes a read-through Go module proxy cache.
package golang

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("go",
	dix.Providers(
		dix.Provider5[*Service, config.Config, meta.Store, object.Store, *slog.Logger, events.Bus](
			func(cfg config.Config, metadata meta.Store, objects object.Store, logger *slog.Logger, bus events.Bus) *Service {
				return NewService(ServiceDependencies{
					Config:   cfg,
					Metadata: metadata,
					Objects:  objects,
					Logger:   logger,
					Events:   bus,
				})
			},
		),
		dix.Provider5[*runtimeAdapter, *Service, *ecosystem.EndpointProber, meta.Store, *worker.Pools, *slog.Logger](newRuntimeAdapter, dix.Into[ecosystem.Runtime](dix.Key("go"), dix.Order(20))),
		dix.Provider1[*Endpoint, *Service](NewEndpoint, dix.Into[httpx.Endpoint](dix.Key("go"), dix.Order(20))),
	),
)
