package mavenproxy

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

var Module = dix.NewModule("maven-proxy",
	dix.Providers(
		dix.Provider3[ServiceDependencies, config.Config, *artifactcache.Store, *slog.Logger](newServiceDependencies),
		dix.Provider1[*Service, ServiceDependencies](NewService),
		dix.Provider1[*runtimeAdapter, *Service](newRuntimeAdapter, dix.Into[ecosystem.Runtime](dix.Key("maven-proxy"), dix.Order(32))),
		dix.Provider1[*Endpoint, *Service](NewEndpoint, dix.Into[httpx.Endpoint](dix.Key("maven-proxy"), dix.Order(32))),
	),
)

func newServiceDependencies(cfg config.Config, cache *artifactcache.Store, logger *slog.Logger) ServiceDependencies {
	return ServiceDependencies{
		Config: cfg,
		Cache:  cache,
		Logger: logger,
	}
}
