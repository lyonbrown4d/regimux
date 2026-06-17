// Package container wires the OCI / Docker Registry ecosystem runtime.
package container

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	containerauth "github.com/lyonbrown4d/regimux/internal/ecosystems/container/auth"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/dockerintegration"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/prefetch"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/registrytool"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/suggestion"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("container",
	dix.Imports(
		registrytool.Module,
		upstream.Module,
		cache.Module,
		containerauth.Module,
		suggestion.Module,
		dockerintegration.Module,
	),
	dix.Providers(
		dix.Provider6[*prefetch.Service, meta.Store, cache.TagService, cache.ManifestService, cache.BlobService, *slog.Logger, *worker.Pools](
			NewPrefetchService,
		),
		dix.Provider5[*Runtime, config.Config, *upstream.Client, *prefetch.Service, cache.ManifestService, *cache.CleanupService](
			NewRuntime,
			dix.Into[ecosystem.Runtime](dix.Key("container"), dix.Order(0)),
		),
		dix.Provider6[RegistryEndpointOptions, config.Config, *observability.Metrics, suggestion.ManifestService, meta.Store, events.Bus, *worker.Pools](newRegistryEndpointOptions),
		dix.Provider6[*RegistryEndpoint, cache.ManifestService, cache.BlobService, cache.TagService, cache.ReferrerService, *slog.Logger, RegistryEndpointOptions](
			NewRegistryEndpointFromOptions,
			dix.Into[httpx.Endpoint](dix.Key("registry"), dix.Order(10)),
		),
	),
)

func NewPrefetchService(
	metadata meta.Store,
	tags cache.TagService,
	manifests cache.ManifestService,
	blobs cache.BlobService,
	logger *slog.Logger,
	pools *worker.Pools,
) *prefetch.Service {
	return prefetch.NewService(prefetch.ServiceDependencies{
		Metadata:  metadata,
		Tags:      tags,
		Manifests: manifests,
		Blobs:     blobs,
		Logger:    logger,
		Workers:   pools,
	})
}

func newRegistryEndpointOptions(
	cfg config.Config,
	metrics *observability.Metrics,
	suggestions suggestion.ManifestService,
	metadata meta.Store,
	bus events.Bus,
	pools *worker.Pools,
) RegistryEndpointOptions {
	return RegistryEndpointOptions{
		Config:      cfg,
		Metrics:     metrics,
		Suggestions: suggestions,
		Metadata:    metadata,
		Events:      bus,
		Workers:     pools,
	}
}
