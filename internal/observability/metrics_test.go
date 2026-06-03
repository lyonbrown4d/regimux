package observability_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/upstream"
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

	metrics.ObserveStaticConfig(context.Background(), cfg, build.Version("test"))

	buildInfo := findMetric(t, recorder.gauges, "service_build_info")
	assertMetricAttr(t, buildInfo, "version", "test")
	upstreams := findMetric(t, recorder.gauges, "service_config_upstreams")
	if upstreams.value != 1 {
		t.Fatalf("upstreams gauge = %f, want 1", upstreams.value)
	}
	endpoint := findMetric(t, recorder.gauges, "service_config_upstream_endpoint")
	assertMetricAttr(t, endpoint, "alias", "hub")
}

func TestAdditionalMetricsRecordBytesAndReports(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)

	metrics.ObserveAPIRequest(context.Background(), "registry.manifest", http.MethodGet, http.StatusOK, time.Millisecond, 128, nil)
	metrics.ObserveCacheStore(context.Background(), "blob", "hub", "library/alpine", 256)
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
	findMetric(t, recorder.gauges, "service_scheduler_cleanup_last_run_bytes")
	findMetric(t, recorder.gauges, "service_scheduler_prefetch_last_run_bytes")
}

func TestObserveUpstreamSnapshotRecordsEndpointHealth(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	snapshot := upstream.ClientSnapshot{Upstreams: []upstream.UpstreamSnapshot{
		{
			Alias: "hub",
			Endpoints: []upstream.EndpointSnapshot{
				{
					Registry: "https://mirror.example.com",
					Role:     "mirror",
					Health: upstream.EndpointHealthSnapshot{
						LatencyEWMA:         25 * time.Millisecond,
						HasLatency:          true,
						ConsecutiveFailures: 2,
						Score:               50 * time.Millisecond,
						HasSuccessRate:      true,
						SuccessRate:         0.75,
					},
				},
			},
		},
	}}

	metrics.ObserveUpstreamSnapshot(context.Background(), snapshot)

	latency := findMetric(t, recorder.gauges, "service_upstream_endpoint_latency_seconds")
	assertMetricAttr(t, latency, "alias", "hub")
	assertMetricAttr(t, latency, "registry", "https://mirror.example.com")
	if latency.value <= 0 {
		t.Fatalf("endpoint latency = %f, want positive", latency.value)
	}
}
