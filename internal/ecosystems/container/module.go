// Package container wires the OCI / Docker Registry ecosystem runtime.
package container

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/prefetch"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/suggestion"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

type PrefetchServiceDependencies struct {
	Metadata  meta.Store
	Tags      cache.TagService
	Manifests cache.ManifestService
	Blobs     cache.BlobService
	Logger    *slog.Logger
	Pools     *worker.Pools
}

var Module = dix.NewModule("container",
	dix.Providers(
		dix.Provider6[PrefetchServiceDependencies, meta.Store, cache.TagService, cache.ManifestService, cache.BlobService, *slog.Logger, *worker.Pools](
			newPrefetchServiceDependencies,
		),
		dix.Provider1[*prefetch.Service, PrefetchServiceDependencies](NewPrefetchService),
		dix.Provider4[*Runtime, config.Config, *upstream.Client, *prefetch.Service, *cache.CleanupService](
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

func NewPrefetchService(deps PrefetchServiceDependencies) *prefetch.Service {
	return prefetch.NewService(prefetch.ServiceDependencies{
		Metadata:  deps.Metadata,
		Tags:      deps.Tags,
		Manifests: deps.Manifests,
		Blobs:     deps.Blobs,
		Logger:    deps.Logger,
		Workers:   deps.Pools,
	})
}

func newPrefetchServiceDependencies(
	metadata meta.Store,
	tags cache.TagService,
	manifests cache.ManifestService,
	blobs cache.BlobService,
	logger *slog.Logger,
	pools *worker.Pools,
) PrefetchServiceDependencies {
	return PrefetchServiceDependencies{
		Metadata:  metadata,
		Tags:      tags,
		Manifests: manifests,
		Blobs:     blobs,
		Logger:    logger,
		Pools:     pools,
	}
}

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
