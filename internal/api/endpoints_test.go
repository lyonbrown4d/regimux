package api_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

func TestServerExposesPrometheusText(t *testing.T) {
	metrics := observability.NewMetrics(nil)
	metrics.ObserveAPIRequest(context.Background(), "registry.manifest", http.MethodGet, http.StatusOK, time.Millisecond, 2, nil)
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
	baseURL := startAPIServerWithOptions(t, api.Options{Metrics: metrics})

	resp := httpGet(t, baseURL+"/metrics")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("regimux_service_api_requests_total")) {
		t.Fatalf("metrics body missing api counter: %q", body)
	}
	if !bytes.Contains(body, []byte("regimux_service_dependency_proxy_pulls_total")) {
		t.Fatalf("metrics body missing dependency pull counter: %q", body)
	}
	if !bytes.Contains(body, []byte("regimux_service_dependency_proxy_policy_denied_pulls_total")) {
		t.Fatalf("metrics body missing dependency policy denied counter: %q", body)
	}
}
