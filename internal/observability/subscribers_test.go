package observability_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

func TestDependencyPulledMetricsSubscriberRecordsPull(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	bus := events.NewBus(slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := bus.Close(); err != nil {
			t.Fatalf("close bus: %v", err)
		}
	})

	unsubscribe, err := observability.NewDependencyPulledMetricsSubscriber(metrics).Subscribe(bus)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	t.Cleanup(unsubscribe)

	if err := events.Publish(context.Background(), bus, events.DependencyPulled{
		Ecosystem:  "container",
		Kind:       "manifest",
		Alias:      "hub",
		Repository: "library/alpine",
		Reference:  "latest",
		Status:     "hit",
	}); err != nil {
		t.Fatalf("publish dependency pulled: %v", err)
	}

	counter := findMetric(t, recorder.counters, "service_dependency_proxy_pulls_total")
	assertMetricAttr(t, counter, "ecosystem", "container")
	assertMetricAttr(t, counter, "kind", "manifest")
	assertMetricAttr(t, counter, "alias", "hub")
	assertMetricAttr(t, counter, "repository", "library/alpine")
	assertMetricAttr(t, counter, "status", "hit")
}

func TestDependencyPullDeniedMetricsSubscriberRecordsPolicyDenial(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	bus := events.NewBus(slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := bus.Close(); err != nil {
			t.Fatalf("close bus: %v", err)
		}
	})

	unsubscribe, err := observability.NewDependencyPullDeniedMetricsSubscriber(metrics).Subscribe(bus)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	t.Cleanup(unsubscribe)

	if err := events.Publish(context.Background(), bus, events.DependencyPullDenied{
		Ecosystem:  "npm",
		Kind:       "metadata",
		Alias:      "npmjs",
		Repository: "left-pad",
		Reference:  "latest",
		Reason:     "dependency blocked",
	}); err != nil {
		t.Fatalf("publish dependency pull denied: %v", err)
	}

	counter := findMetric(t, recorder.counters, "service_dependency_proxy_policy_denied_pulls_total")
	assertMetricAttr(t, counter, "ecosystem", "npm")
	assertMetricAttr(t, counter, "kind", "metadata")
	assertMetricAttr(t, counter, "alias", "npmjs")
	assertMetricAttr(t, counter, "repository", "left-pad")
}

func TestContainerPullMetricsSubscribersRecordLowCardinalityLabels(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	bus := events.NewBus(slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := bus.Close(); err != nil {
			t.Fatalf("close bus: %v", err)
		}
	})

	subscribers := []events.Subscriber{
		observability.NewContainerPullCacheAccessMetricsSubscriber(metrics),
		observability.NewContainerPullStreamCacheFallbackMetricsSubscriber(metrics),
		observability.NewContainerPullFillMetricsSubscriber(metrics),
	}
	for _, subscriber := range subscribers {
		unsubscribe, err := subscriber.Subscribe(bus)
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		t.Cleanup(unsubscribe)
	}

	if err := events.Publish(context.Background(), bus, events.ContainerPullCacheAccess{
		Kind:        "manifest",
		Alias:       "hub",
		CacheStatus: "hit",
	}); err != nil {
		t.Fatalf("publish cache access: %v", err)
	}
	if err := events.Publish(context.Background(), bus, events.ContainerPullStreamCacheFallback{
		Alias:  "hub",
		Reason: "scheduler saturated",
	}); err != nil {
		t.Fatalf("publish stream fallback: %v", err)
	}
	if err := events.Publish(context.Background(), bus, events.ContainerPullFill{
		Alias:  "hub",
		Source: "worker",
		Kind:   "blob",
		Status: "saturated",
		Reason: "worker pool saturated",
	}); err != nil {
		t.Fatalf("publish fill: %v", err)
	}

	cacheAccess := findMetric(t, recorder.counters, "service_container_pull_cache_accesses_total")
	assertMetricAttr(t, cacheAccess, "alias", "hub")
	assertMetricAttr(t, cacheAccess, "kind", "manifest")
	assertMetricAttr(t, cacheAccess, "cache_status", "hit")
	assertNoMetricAttr(t, cacheAccess, "repository")
	assertNoMetricAttr(t, cacheAccess, "digest")

	streamFallback := findMetric(t, recorder.counters, "service_container_pull_stream_cache_fallbacks_total")
	assertMetricAttr(t, streamFallback, "alias", "hub")
	assertMetricAttr(t, streamFallback, "reason", "scheduler_saturated")
	assertNoMetricAttr(t, streamFallback, "repository")
	assertNoMetricAttr(t, streamFallback, "digest")

	fill := findMetric(t, recorder.counters, "service_container_pull_fills_total")
	assertMetricAttr(t, fill, "alias", "hub")
	assertMetricAttr(t, fill, "source", "worker")
	assertMetricAttr(t, fill, "kind", "blob")
	assertMetricAttr(t, fill, "status", "saturated")
	assertMetricAttr(t, fill, "reason", "worker_pool_saturated")
	assertNoMetricAttr(t, fill, "repository")
	assertNoMetricAttr(t, fill, "digest")
}
