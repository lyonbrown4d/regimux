package npm

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("npm-proxy",
	dix.Providers(
		dix.Provider4[ServiceDependencies, config.Config, *artifactcache.Store, meta.Store, *slog.Logger](newServiceDependencies),
		dix.Provider1[*Service, ServiceDependencies](NewService),
		dix.Provider5[*runtimeAdapter, *Service, *ecosystem.EndpointProber, meta.Store, *worker.Pools, *slog.Logger](newRuntimeAdapter, dix.Into[ecosystem.Runtime](dix.Key("npm-proxy"), dix.Order(30))),
		dix.Provider1[*Endpoint, *Service](NewEndpoint, dix.Into[httpx.Endpoint](dix.Key("npm-proxy"), dix.Order(30))),
	),
)

func newServiceDependencies(cfg config.Config, cache *artifactcache.Store, metadata meta.Store, logger *slog.Logger) ServiceDependencies {
	return ServiceDependencies{
		Config:   cfg,
		Cache:    cache,
		Metadata: metadata,
		Logger:   logger,
	}
}
