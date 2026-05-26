package api

import (
	"log/slog"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
)

func EndpointsModule(configModule, cacheModule, observabilityModule dix.Module) dix.Module {
	return dix.NewModule("api-endpoints",
		dix.Imports(configModule, cacheModule, observabilityModule),
		dix.Providers(
			dix.Provider0[*HealthEndpoint](NewHealthEndpoint,
				dix.Into[httpx.Endpoint](dix.Key("health"), dix.Order(-100)),
			),
			dix.Provider6[*RegistryEndpoint, cache.ManifestService, cache.BlobService, cache.TagService, cache.ReferrerService, *slog.Logger, config.Config](
				NewRegistryEndpointFromConfig,
				dix.Into[httpx.Endpoint](dix.Key("registry"), dix.Order(10)),
			),
		),
	)
}

func Module(configModule, observabilityModule, eventsModule, endpointModule dix.Module) dix.Module {
	return dix.NewModule("api",
		dix.Imports(configModule, observabilityModule, eventsModule, endpointModule),
		dix.Providers(
			dix.Provider4[*Server, config.Config, *slog.Logger, events.Bus, *collectionlist.List[httpx.Endpoint]](
				newServer,
				dix.Eager(),
			),
		),
	)
}

func newServer(cfg config.Config, logger *slog.Logger, _ events.Bus, endpoints *collectionlist.List[httpx.Endpoint]) *Server {
	var values []httpx.Endpoint
	if endpoints != nil {
		values = endpoints.Values()
	}
	return NewServer(Options{
		Listen:      cfg.Server.Listen,
		PublicURL:   cfg.Server.PublicURL,
		Logger:      logger,
		Endpoints:   values,
		PrintRoutes: false,
	})
}
