package observability

import (
	"context"
	"strings"

	"github.com/arcgolabs/observabilityx"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
)

type configMetrics struct {
	buildInfo          observabilityx.Gauge
	cacheBackend       observabilityx.Gauge
	storeBackend       observabilityx.Gauge
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
		upstreams: obs.Gauge(gaugeSpec(
			"config_upstreams",
			"Configured upstream aliases.",
		)),
		upstreamEndpoint: obs.Gauge(gaugeSpec(
			"config_upstream_endpoint",
			"Configured upstream endpoint.",
			"alias", "registry", "role",
		)),
		schedulerComponent: obs.Gauge(gaugeSpec(
			"config_scheduler_component",
			"Configured scheduler component.",
			"component", "enabled",
		)),
	}
}

func ObserveStaticConfig(ctx context.Context, cfg config.Config, version build.Version, metrics *Metrics) error {
	if metrics == nil {
		return nil
	}
	metrics.ObserveStaticConfig(ctx, cfg, version)
	return nil
}

func (m *Metrics) ObserveStaticConfig(ctx context.Context, cfg config.Config, version build.Version) {
	if m == nil {
		return
	}
	m.config.buildInfo.Set(ctx, 1, observabilityx.String("version", string(version)))
	m.config.cacheBackend.Set(ctx, 1, observabilityx.String("backend", labelOrUnknown(cfg.Cache.Backend)))
	m.config.storeBackend.Set(ctx, 1, observabilityx.String("kind", "meta"), observabilityx.String("driver", labelOrUnknown(cfg.Store.Meta.Driver)))
	m.config.storeBackend.Set(ctx, 1, observabilityx.String("kind", "object"), observabilityx.String("driver", labelOrUnknown(cfg.Store.Object.Driver)))
	m.config.upstreams.Set(ctx, float64(len(cfg.Upstreams)))
	observeConfiguredUpstreamEndpoints(ctx, m.config.upstreamEndpoint, cfg)
	m.config.schedulerComponent.Set(ctx, boolFloat(cfg.Scheduler.Enabled), schedulerConfigLabels("scheduler", cfg.Scheduler.Enabled)...)
	m.config.schedulerComponent.Set(ctx, boolFloat(cfg.Scheduler.Cleanup.Enabled), schedulerConfigLabels("cleanup", cfg.Scheduler.Cleanup.Enabled)...)
	m.config.schedulerComponent.Set(ctx, boolFloat(cfg.Scheduler.Prefetch.Enabled), schedulerConfigLabels("prefetch", cfg.Scheduler.Prefetch.Enabled)...)
}

func observeConfiguredUpstreamEndpoints(ctx context.Context, metric observabilityx.Gauge, cfg config.Config) {
	cfg.OrderedUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		for _, endpoint := range configuredUpstreamEndpoints(upstreamCfg) {
			metric.Set(ctx, 1,
				observabilityx.String("alias", alias),
				observabilityx.String("registry", endpoint.registry),
				observabilityx.String("role", endpoint.role),
			)
		}
		return true
	})
}

type configuredEndpoint struct {
	registry string
	role     string
}

func configuredUpstreamEndpoints(cfg config.UpstreamConfig) []configuredEndpoint {
	endpoints := make([]configuredEndpoint, 0, len(cfg.Mirrors)+1)
	for _, mirror := range cfg.Mirrors {
		if registry := cleanMetricRegistry(mirror); registry != "" {
			endpoints = append(endpoints, configuredEndpoint{registry: registry, role: "mirror"})
		}
	}
	if registry := cleanMetricRegistry(cfg.Registry); registry != "" {
		endpoints = append(endpoints, configuredEndpoint{registry: registry, role: "primary"})
	}
	return endpoints
}

func cleanMetricRegistry(registry string) string {
	return strings.TrimRight(strings.TrimSpace(registry), "/")
}

func schedulerConfigLabels(component string, enabled bool) []observabilityx.Attribute {
	return []observabilityx.Attribute{
		observabilityx.String("component", component),
		observabilityx.String("enabled", boolLabel(enabled)),
	}
}
