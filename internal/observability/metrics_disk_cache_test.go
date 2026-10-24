package observability_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestObjectDiskCacheMetricsRecordBoundedOutcomes(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	metrics.ObserveObjectDiskCache(t.Context(), object.DiskCacheEvent{Operation: "get", Result: "hit", Bytes: 1024})
	metric := findMetric(t, recorder.counters, "service_object_disk_cache_accesses_total")
	if metric.value != 1 {
		t.Fatalf("cache outcome count=%f, want one", metric.value)
	}
	assertMetricAttr(t, metric, "operation", "get")
	assertMetricAttr(t, metric, "result", "hit")
}

func TestObjectDiskCacheMetricsDiscardUnknownLabelValues(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	metrics.ObserveObjectDiskCache(t.Context(), object.DiskCacheEvent{
		Operation: "private/bucket/key", Result: "secret-error-detail",
	})
	metric := findMetric(t, recorder.counters, "service_object_disk_cache_accesses_total")
	assertMetricAttr(t, metric, "operation", "other")
	assertMetricAttr(t, metric, "result", "other")
}

func TestObjectDiskCacheMetricsScrapeCurrentStats(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	stats := object.DiskCacheStats{Bytes: 1024, ReservedBytes: 2048, Entries: 2, Fills: 1}
	metrics.RegisterObjectDiskCacheStats(func() object.DiskCacheStats { return stats })
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	metrics.Handler().ServeHTTP(httptest.NewRecorder(), request)
	for name, value := range map[string]float64{
		"service_object_disk_cache_bytes":          1024,
		"service_object_disk_cache_reserved_bytes": 2048,
		"service_object_disk_cache_entries":        2,
		"service_object_disk_cache_fills":          1,
	} {
		metric := findMetric(t, recorder.gauges, name)
		if metric.value != value || len(metric.attrs) != 0 {
			t.Fatalf("cache resource gauge %s=%+v, want %f with no object labels", name, metric, value)
		}
	}
	stats.Bytes = 512
	recorder.gauges = nil
	metrics.Handler().ServeHTTP(httptest.NewRecorder(), request)
	if metric := findMetric(t, recorder.gauges, "service_object_disk_cache_bytes"); metric.value != 512 {
		t.Fatalf("disk cache byte gauge did not refresh: %+v", metric)
	}
}
