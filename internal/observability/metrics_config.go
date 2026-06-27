package observability

import (
	"context"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/observabilityx"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/lo"
)

type configMetrics struct {
	buildInfo          observabilityx.Gauge
	cacheBackend       observabilityx.Gauge
	storeBackend       observabilityx.Gauge
	dockerIntegration  observabilityx.Gauge
	upstreams          observabilityx.Gauge
	upstreamEndpoint   observabilityx.Gauge
	schedulerComponent observabilityx.Gauge
}

func newConfigMetrics(obs observabilityx.Observability) configMetrics {
	return configMetrics{
		buildInfo: obs.Gauge(gaugeSpec(
			"build_info",
			"Build information for the running RegiMux process.",
			"version",
		)),
		cacheBackend: obs.Gauge(gaugeSpec(
			"config_cache_backend",
			"Configured cache backend.",
			"backend",
		)),
		storeBackend: obs.Gauge(gaugeSpec(
			"config_store_backend",
			"Configured store backend.",
			"kind", "driver",
		)),
		dockerIntegration: obs.Gauge(gaugeSpec(
			"config_docker_integration",
			"Configured optional Docker daemon integration.",
			"component", "enabled",
		)),
		upstreams: obs.Gauge(gaugeSpec(
			"config_upstreams",
			"Configured upstream aliases.",
		)),
		upstreamEndpoint: obs.Gauge(gaugeSpec(
			"config_upstream_endpoint",
			"Configured upstream endpoint.",
			"ecosystem", "alias", "registry", "role",
		)),
		schedulerComponent: obs.Gauge(gaugeSpec(
			"config_scheduler_component",
			"Configured scheduler component.",
			"component", "enabled",
		)),
	}
}

func ObserveStaticConfig(ctx context.Context, deps StaticConfigDependencies) error {
	if deps.Metrics == nil {
		return nil
	}
	deps.Metrics.ObserveStaticConfigWithRuntimes(ctx, deps.Config, deps.Version, deps.Runtimes)
	return nil
}

func (m *Metrics) ObserveStaticConfig(ctx context.Context, cfg config.Config, version build.Version) {
	m.ObserveStaticConfigWithRuntimes(ctx, cfg, version, nil)
}

func (m *Metrics) ObserveStaticConfigWithRuntimes(
	ctx context.Context,
	cfg config.Config,
	version build.Version,
	runtimes *collectionlist.List[ecosystem.Runtime],
) {
	if m == nil {
		return
	}
	upstreams := ecosystem.ConfiguredUpstreams(runtimes)
	m.config.buildInfo.Set(ctx, 1, observabilityx.String("version", string(version)))
	m.config.cacheBackend.Set(ctx, 1, observabilityx.String("backend", labelOrUnknown(cfg.Cache.Backend)))
	m.config.storeBackend.Set(ctx, 1, observabilityx.String("kind", "meta"), observabilityx.String("driver", labelOrUnknown(cfg.Store.Meta.Driver)))
	m.config.storeBackend.Set(ctx, 1, observabilityx.String("kind", "object"), observabilityx.String("driver", labelOrUnknown(cfg.Store.Object.Driver)))
	m.config.dockerIntegration.Set(ctx, boolFloat(cfg.Docker.Enabled), dockerConfigLabels("integration", cfg.Docker.Enabled).Values()...)
	m.config.dockerIntegration.Set(ctx, boolFloat(cfg.Docker.Enabled && cfg.Docker.Observe), dockerConfigLabels("observe", cfg.Docker.Enabled && cfg.Docker.Observe).Values()...)
	m.config.dockerIntegration.Set(ctx, boolFloat(cfg.Docker.Enabled && cfg.Docker.Prewarm.Enabled), dockerConfigLabels("prewarm", cfg.Docker.Enabled && cfg.Docker.Prewarm.Enabled).Values()...)
	m.config.upstreams.Set(ctx, float64(upstreams.Len()))
	observeConfiguredUpstreamEndpoints(ctx, m.config.upstreamEndpoint, upstreams)
	m.config.schedulerComponent.Set(ctx, boolFloat(cfg.Scheduler.Enabled), schedulerConfigLabels("scheduler", cfg.Scheduler.Enabled).Values()...)
	m.config.schedulerComponent.Set(ctx, boolFloat(cfg.Scheduler.Cleanup.Enabled), schedulerConfigLabels("cleanup", cfg.Scheduler.Cleanup.Enabled).Values()...)
	m.config.schedulerComponent.Set(ctx, boolFloat(cfg.Scheduler.Prefetch.Enabled), schedulerConfigLabels("prefetch", cfg.Scheduler.Prefetch.Enabled).Values()...)
}

func observeConfiguredUpstreamEndpoints(ctx context.Context, metric observabilityx.Gauge, upstreams *collectionlist.List[ecosystem.Upstream]) {
	if upstreams == nil {
		return
	}
	upstreams.Range(func(_ int, upstream ecosystem.Upstream) bool {
		endpoints := configuredUpstreamEndpoints(collectionlist.NewList(upstream.Config.Mirrors...), upstream.Config.Registry)
		if endpoints == nil {
			return true
		}
		endpoints.Range(func(_ int, endpoint configuredEndpoint) bool {
			metric.Set(ctx, 1,
				observabilityx.String("ecosystem", upstream.Ecosystem),
				observabilityx.String("alias", upstream.Alias),
				observabilityx.String("registry", endpoint.registry),
				observabilityx.String("role", endpoint.role),
			)
			return true
		})
		return true
	})
}

type configuredEndpoint struct {
	registry string
	role     string
}

func configuredUpstreamEndpoints(mirrors *collectionlist.List[string], primaryRegistry string) *collectionlist.List[configuredEndpoint] {
	mirrorValues := []string(nil)
	if mirrors != nil {
		mirrorValues = mirrors.Values()
	}
	mirrorCount := len(mirrorValues)
	all := append(append([]string(nil), mirrorValues...), primaryRegistry)
	endpoints := lo.FilterMap(all, func(endpoint string, index int) (configuredEndpoint, bool) {
		registry := cleanMetricRegistry(endpoint)
		role := "primary"
		if index < mirrorCount {
			role = "mirror"
		}
		return configuredEndpoint{registry: registry, role: role}, registry != ""
	})
	if len(endpoints) == 0 {
		return nil
	}
	return collectionlist.NewList(endpoints...)
}

func cleanMetricRegistry(registry string) string {
	return strings.TrimRight(strings.TrimSpace(registry), "/")
}

func schedulerConfigLabels(component string, enabled bool) *collectionlist.List[observabilityx.Attribute] {
	return collectionlist.NewList(
		observabilityx.String("component", component),
		observabilityx.String("enabled", boolLabel(enabled)),
	)
}

func dockerConfigLabels(component string, enabled bool) *collectionlist.List[observabilityx.Attribute] {
	return collectionlist.NewList(
		observabilityx.String("component", component),
		observabilityx.String("enabled", boolLabel(enabled)),
	)
}
