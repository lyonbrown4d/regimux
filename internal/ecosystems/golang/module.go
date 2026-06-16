// Package golang exposes a read-through Go module proxy cache.
package golang

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("go",
	dix.Providers(
		dix.Provider6[*Service, config.Config, *artifactcache.Store, meta.Store, *clientfactory.Factory, *slog.Logger, events.Bus](
			func(cfg config.Config, cache *artifactcache.Store, metadata meta.Store, factory *clientfactory.Factory, logger *slog.Logger, bus events.Bus) *Service {
				return NewService(ServiceDependencies{
					Config:   cfg,
					Cache:    cache,
					Metadata: metadata,
					Factory:  factory,
					Logger:   logger,
					Events:   bus,
				})
			},
		),
		dix.Provider5[*runtimeAdapter, *Service, *ecosystem.EndpointProber, meta.Store, *worker.Pools, *slog.Logger](newRuntimeAdapter, dix.Into[ecosystem.Runtime](dix.Key("go"), dix.Order(20))),
		dix.Provider1[*Endpoint, *Service](NewEndpoint, dix.Into[httpx.Endpoint](dix.Key("go"), dix.Order(20))),
		dix.Provider1[*RootEndpoint, *Service](NewRootEndpoint, dix.Into[httpx.Endpoint](dix.Key("go-root"), dix.Order(1000))),
	),
)
