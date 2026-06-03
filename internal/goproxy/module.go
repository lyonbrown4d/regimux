// Package goproxy exposes a read-through Go module proxy cache.
package goproxy

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("go-proxy",
	dix.Providers(
		dix.Provider4[ServiceDependencies, config.Config, meta.Store, object.Store, *slog.Logger](newServiceDependencies),
		dix.Provider1[*Service, ServiceDependencies](NewService),
		dix.Provider5[*runtimeAdapter, *Service, *ecosystem.EndpointProber, meta.Store, *worker.Pools, *slog.Logger](newRuntimeAdapter, dix.Into[ecosystem.Runtime](dix.Key("go-proxy"), dix.Order(20))),
		dix.Provider1[*Endpoint, *Service](NewEndpoint, dix.Into[httpx.Endpoint](dix.Key("go-proxy"), dix.Order(20))),
	),
)

func newServiceDependencies(cfg config.Config, metadata meta.Store, objects object.Store, logger *slog.Logger) ServiceDependencies {
	return ServiceDependencies{
		Config:   cfg,
		Metadata: metadata,
		Objects:  objects,
		Logger:   logger,
	}
}
