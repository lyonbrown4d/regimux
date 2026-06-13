// Package dist exposes a read-through binary distribution mirror.
package dist

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("dist",
	dix.Providers(
		dix.Provider6[*Service, config.Config, meta.Store, object.Store, *clientfactory.Factory, *slog.Logger, events.Bus](
			func(cfg config.Config, metadata meta.Store, objects object.Store, factory *clientfactory.Factory, logger *slog.Logger, bus events.Bus) *Service {
				return NewService(ServiceDependencies{
					Config:   cfg,
					Metadata: metadata,
					Objects:  objects,
					Factory:  factory,
					Logger:   logger,
					Events:   bus,
				})
			},
		),
		dix.Provider5[*runtimeAdapter, *Service, *ecosystem.EndpointProber, meta.Store, *worker.Pools, *slog.Logger](newRuntimeAdapter, dix.Into[ecosystem.Runtime](dix.Key("dist"), dix.Order(60))),
		dix.Provider1[*Endpoint, *Service](NewEndpoint, dix.Into[httpx.Endpoint](dix.Key("dist"), dix.Order(60))),
	),
)
