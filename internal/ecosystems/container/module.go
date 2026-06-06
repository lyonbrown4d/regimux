// Package container wires the OCI / Docker Registry ecosystem runtime.
package container

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/suggestion"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
)

var Module = dix.NewModule("container",
	dix.Providers(
		dix.Provider3[*Runtime, config.Config, *upstream.Client, *prefetch.Service](
			NewRuntime,
			dix.Into[ecosystem.Runtime](dix.Key("container"), dix.Order(0)),
		),
		dix.Provider3[RegistryEndpointOptions, config.Config, *observability.Metrics, suggestion.ManifestService](newRegistryEndpointOptions),
		dix.Provider6[*RegistryEndpoint, cache.ManifestService, cache.BlobService, cache.TagService, cache.ReferrerService, *slog.Logger, RegistryEndpointOptions](
			NewRegistryEndpointFromOptions,
			dix.Into[httpx.Endpoint](dix.Key("registry"), dix.Order(10)),
		),
	),
)

func newRegistryEndpointOptions(
	cfg config.Config,
	metrics *observability.Metrics,
	suggestions suggestion.ManifestService,
) RegistryEndpointOptions {
	return RegistryEndpointOptions{
		Config:      cfg,
		Metrics:     metrics,
		Suggestions: suggestions,
	}
}
