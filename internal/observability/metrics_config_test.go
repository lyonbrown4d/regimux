package observability_test

import (
	"context"
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

func TestConfiguredUpstreamEndpointsOrdersMirrorsThenPrimary(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	cfg := config.DefaultConfig()
	cfg.Container = config.ContainerConfig{
		"hub": {
			Registry: "https://registry.example.com/",
			Mirrors:  []string{"https://mirror-1.example.com/", "  https://mirror-2.example.com"},
		},
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	runtimes := collectionlist.NewList[ecosystem.Runtime](
		ecosystem.NewConfigRuntime(ecosystem.Container, cfg.OrderedContainerUpstreams()),
	)
	metrics.ObserveStaticConfigWithRuntimes(context.Background(), cfg, build.Version("test"), runtimes)

	endpoints := metricsForName(recorder.gauges, "service_config_upstream_endpoint")
	if len(endpoints) != 3 {
		t.Fatalf("len = %d, want 3", len(endpoints))
	}
	if got, want := endpoints[0].attrs["registry"], "https://mirror-1.example.com"; got != want {
		t.Fatalf("endpoint0 registry = %q, want %q", got, want)
	}
	if got, want := endpoints[1].attrs["registry"], "https://mirror-2.example.com"; got != want {
		t.Fatalf("endpoint1 registry = %q, want %q", got, want)
	}
	if got, want := endpoints[2].attrs["registry"], "https://registry.example.com"; got != want {
		t.Fatalf("endpoint2 registry = %q, want %q", got, want)
	}
	if got, want := endpoints[0].attrs["role"], "mirror"; got != want {
		t.Fatalf("endpoint0 role = %q, want %q", got, want)
	}
	if got, want := endpoints[1].attrs["role"], "mirror"; got != want {
		t.Fatalf("endpoint1 role = %q, want %q", got, want)
	}
	if got, want := endpoints[2].attrs["role"], "primary"; got != want {
		t.Fatalf("endpoint2 role = %q, want %q", got, want)
	}
}

func TestConfiguredUpstreamEndpointsSkipsBlankValues(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	cfg := config.DefaultConfig()
	cfg.Container = config.ContainerConfig{
		"hub": {
			Registry: "   ",
			Mirrors:  []string{"https://mirror.example.com/"},
		},
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	runtimes := collectionlist.NewList[ecosystem.Runtime](
		ecosystem.NewConfigRuntime(ecosystem.Container, cfg.OrderedContainerUpstreams()),
	)
	metrics.ObserveStaticConfigWithRuntimes(context.Background(), cfg, build.Version("test"), runtimes)

	endpoints := metricsForName(recorder.gauges, "service_config_upstream_endpoint")
	if len(endpoints) != 1 {
		t.Fatalf("len = %d, want 1", len(endpoints))
	}
	if got, want := endpoints[0].attrs["registry"], "https://mirror.example.com"; got != want {
		t.Fatalf("endpoint registry = %q, want %q", got, want)
	}
	if got, want := endpoints[0].attrs["role"], "mirror"; got != want {
		t.Fatalf("endpoint role = %q, want %q", got, want)
	}
}

func metricsForName(records []metricRecord, name string) []metricRecord {
	out := make([]metricRecord, 0, len(records))
	for _, record := range records {
		if record.name == name {
			out = append(out, record)
		}
	}
	return out
}
