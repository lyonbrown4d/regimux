package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestRegistryEndpointAppliesDefaultNamespace(t *testing.T) {
	manifests := &recordingManifestService{}
	endpoint := api.NewRegistryEndpointFromConfig(
		manifests,
		nil,
		nil,
		nil,
		nil,
		config.Config{
			Upstreams: map[string]config.UpstreamConfig{
				"hub": {DefaultNamespace: "library"},
			},
		},
	)
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/hello-world/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	if !bytes.Equal(body, []byte("{}")) {
		t.Fatalf("body = %q, want {}", body)
	}
	if manifests.Repo() != "library/hello-world" {
		t.Fatalf("repo = %q, want library/hello-world", manifests.Repo())
	}
}

func TestRegistryEndpointInvalidDigestReturnsDigestInvalid(t *testing.T) {
	baseURL := startAPIServer(t, api.NewRegistryEndpoint(nil, nil, nil, nil, nil))

	resp := httpGet(t, baseURL+"/v2/hub/library/alpine/blobs/not-a-digest")
	bodyBytes := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%q, want 400", resp.StatusCode, bodyBytes)
	}

	var body distribution.ErrorResponse
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("unmarshal error body: %v body=%q", err, bodyBytes)
	}
	if len(body.Errors) != 1 || body.Errors[0].Code != distribution.CodeDigestInvalid {
		t.Fatalf("error body = %#v, want DIGEST_INVALID", body.Errors)
	}
}

func TestMetricsEndpointExposesPrometheusText(t *testing.T) {
	metrics := observability.NewMetrics(nil)
	metrics.ObserveAPIRequest("registry.manifest", http.MethodGet, http.StatusOK, time.Millisecond, nil)
	baseURL := startAPIServer(t, api.NewMetricsEndpoint(metrics))

	resp := httpGet(t, baseURL+"/metrics")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("regimux_service_api_requests_total")) {
		t.Fatalf("metrics body missing api counter: %q", body)
	}
}

type recordingManifestService struct {
	mu   sync.Mutex
	repo string
}

func (s *recordingManifestService) Get(_ context.Context, req cache.ManifestRequest) (*cache.CachedManifest, error) {
	s.mu.Lock()
	s.repo = req.Repo
	s.mu.Unlock()
	return &cache.CachedManifest{
		Digest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Size:      2,
		Body:      []byte("{}"),
		Headers:   http.Header{"Content-Length": {"2"}},
		Cache:     cache.CacheBypass,
	}, nil
}

func (s *recordingManifestService) Repo() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repo
}

var _ cache.ManifestService = (*recordingManifestService)(nil)
