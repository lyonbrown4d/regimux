package observability_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

func TestObserveStaticConfigRecordsConfiguredUpstreamMetrics(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	cfg := config.DefaultConfig()
	cfg.Container = config.ContainerConfig{
		"hub": {
			Registry: "https://registry-1.docker.io/",
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

	buildInfo := findMetric(t, recorder.gauges, "service_build_info")
	assertMetricAttr(t, buildInfo, "version", "test")
	upstreams := findMetric(t, recorder.gauges, "service_config_upstreams")
	if upstreams.value != 1 {
		t.Fatalf("upstreams gauge = %f, want 1", upstreams.value)
	}
	endpoint := findMetric(t, recorder.gauges, "service_config_upstream_endpoint")
	assertMetricAttr(t, endpoint, "ecosystem", "container")
	assertMetricAttr(t, endpoint, "alias", "hub")
}

func TestAdditionalMetricsRecordBytesAndReports(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)

	metrics.ObserveAPIRequest(context.Background(), "registry.manifest", http.MethodGet, http.StatusOK, time.Millisecond, 128, nil)
	metrics.ObserveCacheStore(context.Background(), "blob", "hub", "library/alpine", 256)
	metrics.ObserveDependencyPull(context.Background(), observability.DependencyPullMetric{
		Ecosystem:  "container",
		Kind:       "manifest",
		Alias:      "hub",
		Repository: "library/alpine",
		Status:     "hit",
	})
	metrics.ObserveDependencyPolicyDeniedPull(context.Background(), observability.DependencyPolicyDeniedPullMetric{
		Ecosystem:  "npm",
		Kind:       "metadata",
		Alias:      "npmjs",
		Repository: "left-pad",
	})
	metrics.ObserveCleanupReport(context.Background(), observability.CleanupReportMetrics{ScannedBlobs: 3, BytesDeleted: 512})
	metrics.ObservePrefetchReport(context.Background(), observability.PrefetchReportMetrics{Candidates: 4, BytesWarmed: 1024})

	apiBytes := findMetric(t, recorder.histograms, "service_api_response_bytes")
	if apiBytes.value != 128 {
		t.Fatalf("api response bytes = %f, want 128", apiBytes.value)
	}
	storeObjects := findMetric(t, recorder.counters, "service_cache_store_objects_total")
	if storeObjects.value != 1 {
		t.Fatalf("cache store objects = %f, want 1", storeObjects.value)
	}
	dependencyPulls := findMetric(t, recorder.counters, "service_dependency_proxy_pulls_total")
	assertMetricAttr(t, dependencyPulls, "ecosystem", "container")
	assertMetricAttr(t, dependencyPulls, "status", "hit")
	deniedPulls := findMetric(t, recorder.counters, "service_dependency_proxy_policy_denied_pulls_total")
	assertMetricAttr(t, deniedPulls, "ecosystem", "npm")
	assertMetricAttr(t, deniedPulls, "repository", "left-pad")
	findMetric(t, recorder.gauges, "service_scheduler_cleanup_last_run_bytes")
	findMetric(t, recorder.gauges, "service_scheduler_prefetch_last_run_bytes")
}

func TestObserveUpstreamSnapshotRecordsEndpointHealth(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	snapshot := ecosystem.ClientSnapshot{Upstreams: collectionlist.NewList(
		ecosystem.UpstreamSnapshot{
			Alias: "hub",
			Endpoints: collectionlist.NewList(
				ecosystem.EndpointSnapshot{
					Registry: "https://mirror.example.com",
					Role:     "mirror",
					Health: ecosystem.EndpointHealthSnapshot{
						LatencyEWMA:         25 * time.Millisecond,
						HasLatency:          true,
						ConsecutiveFailures: 2,
						Score:               50 * time.Millisecond,
						HasSuccessRate:      true,
						SuccessRate:         0.75,
					},
				},
			),
		},
	)}

	metrics.ObserveUpstreamSnapshot(context.Background(), snapshot)

	latency := findMetric(t, recorder.gauges, "service_upstream_endpoint_latency_seconds")
	assertMetricAttr(t, latency, "alias", "hub")
	assertMetricAttr(t, latency, "registry", "https://mirror.example.com")
	if latency.value <= 0 {
		t.Fatalf("endpoint latency = %f, want positive", latency.value)
	}
}
