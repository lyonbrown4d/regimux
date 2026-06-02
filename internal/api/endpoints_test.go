package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/suggestion"
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

func TestRegistryEndpointSuggestsSimilarTagsForMissingManifest(t *testing.T) {
	manifests := &recordingManifestService{
		err: distribution.ErrManifestUnknown.WithDetail("missing manifest"),
	}
	suggestions := &recordingSuggestionService{
		result: suggestion.ManifestSuggestions{
			Tags: []string{"latest-pg18", "latest-pg18-oss"},
		},
	}
	endpoint := api.NewRegistryEndpointFromOptions(manifests, nil, nil, nil, nil, api.RegistryEndpointOptions{
		Suggestions: suggestions,
	})
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/timescale/timescaledb/manifests/latest-18")
	bodyBytes := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body=%q, want 404", resp.StatusCode, bodyBytes)
	}

	var body manifestErrorResponse
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("unmarshal error body: %v body=%q", err, bodyBytes)
	}
	if len(body.Errors) != 1 || body.Errors[0].Code != distribution.CodeManifestUnknown {
		t.Fatalf("error body = %#v, want MANIFEST_UNKNOWN", body.Errors)
	}
	if !strings.Contains(body.Errors[0].Message, "latest-pg18") {
		t.Fatalf("message = %q, want tag suggestion", body.Errors[0].Message)
	}
	assertSuggestionRequest(t, suggestions.SuggestRequest(), suggestion.ManifestRequest{
		Alias:      "hub",
		Repository: "timescale/timescaledb",
		Reference:  "latest-18",
	})
	assertManifestSuggestions(t, body.Errors[0].Detail.Suggestions)
}

func TestRegistryEndpointManifestHitDoesNotRequestTagSuggestions(t *testing.T) {
	manifests := &recordingManifestService{}
	suggestions := &recordingSuggestionService{}
	endpoint := api.NewRegistryEndpointFromOptions(manifests, nil, nil, nil, nil, api.RegistryEndpointOptions{
		Suggestions: suggestions,
	})
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/timescale/timescaledb/manifests/latest-18")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	if !bytes.Equal(body, []byte("{}")) {
		t.Fatalf("body = %q, want {}", body)
	}
	if got := suggestions.SuggestCalls(); got != 0 {
		t.Fatalf("suggest calls = %d, want 0", got)
	}
	if got := suggestions.ObserveCalls(); got != 1 {
		t.Fatalf("observe calls = %d, want 1", got)
	}
}

func TestRegistryEndpointHeadManifestMissHasNoSuggestionBody(t *testing.T) {
	manifests := &recordingManifestService{
		err: distribution.ErrManifestUnknown.WithDetail("missing manifest"),
	}
	suggestions := &recordingSuggestionService{
		result: suggestion.ManifestSuggestions{
			Tags: []string{"latest-pg18", "latest-pg18-oss"},
		},
	}
	endpoint := api.NewRegistryEndpointFromOptions(manifests, nil, nil, nil, nil, api.RegistryEndpointOptions{
		Suggestions: suggestions,
	})
	baseURL := startAPIServer(t, endpoint)

	resp := httpHead(t, baseURL+"/v2/hub/timescale/timescaledb/manifests/latest-18")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body=%q, want 404", resp.StatusCode, body)
	}
	// Docker manifest resolution uses HEAD heavily; HTTP HEAD responses do not
	// expose the JSON error body, so Docker cannot display tag suggestions here.
	if len(body) != 0 {
		t.Fatalf("HEAD body = %q, want empty body", body)
	}
	if got := suggestions.SuggestCalls(); got != 1 {
		t.Fatalf("suggest calls = %d, want 1", got)
	}
}

func TestServerExposesPrometheusText(t *testing.T) {
	metrics := observability.NewMetrics(nil)
	metrics.ObserveAPIRequest(context.Background(), "registry.manifest", http.MethodGet, http.StatusOK, time.Millisecond, 2, nil)
	baseURL := startAPIServerWithOptions(t, api.Options{Metrics: metrics})

	resp := httpGet(t, baseURL+"/metrics")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("regimux_service_api_requests_total")) {
		t.Fatalf("metrics body missing api counter: %q", body)
	}
}

func httpHead(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodHead, url, http.NoBody)
	if err != nil {
		t.Fatalf("build HEAD %s: %v", url, err)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HEAD %s: %v", url, err)
	}
	return resp
}

type recordingManifestService struct {
	mu   sync.Mutex
	repo string
	err  error
}

func (s *recordingManifestService) Get(_ context.Context, req cache.ManifestRequest) (*cache.CachedManifest, error) {
	s.mu.Lock()
	s.repo = req.Repo
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return &cache.CachedManifest{
		Digest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MediaType: distribution.MediaTypeOCIManifest,
		Size:      2,
		Body:      []byte("{}"),
		Headers:   http.Header{distribution.HeaderContentLength: {"2"}},
		Cache:     cache.CacheBypass,
	}, nil
}

func (s *recordingManifestService) Repo() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repo
}

var _ cache.ManifestService = (*recordingManifestService)(nil)

type recordingSuggestionService struct {
	mu             sync.Mutex
	result         suggestion.ManifestSuggestions
	suggestRequest suggestion.ManifestRequest
	observeRequest suggestion.ManifestRequest
	suggestCalls   int
	observeCalls   int
}

func (s *recordingSuggestionService) ObserveManifest(_ context.Context, req suggestion.ManifestRequest) {
	s.mu.Lock()
	s.observeRequest = req
	s.observeCalls++
	s.mu.Unlock()
}

func (s *recordingSuggestionService) SuggestManifest(
	_ context.Context,
	req suggestion.ManifestRequest,
) suggestion.ManifestSuggestions {
	s.mu.Lock()
	s.suggestRequest = req
	s.suggestCalls++
	result := s.result
	s.mu.Unlock()
	return result
}

func (s *recordingSuggestionService) SuggestCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.suggestCalls
}

func (s *recordingSuggestionService) ObserveCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.observeCalls
}

func (s *recordingSuggestionService) SuggestRequest() suggestion.ManifestRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.suggestRequest
}

func assertManifestSuggestions(t *testing.T, suggestions []distribution.ManifestSuggestion) {
	t.Helper()
	if len(suggestions) == 0 {
		t.Fatal("expected manifest suggestions")
	}
	first := suggestions[0]
	if first.Reference != "latest-pg18" {
		t.Fatalf("first suggestion reference = %q, want latest-pg18", first.Reference)
	}
	if first.Image != "hub/timescale/timescaledb:latest-pg18" {
		t.Fatalf("first suggestion image = %q, want hub/timescale/timescaledb:latest-pg18", first.Image)
	}
}

func assertSuggestionRequest(t *testing.T, got, want suggestion.ManifestRequest) {
	t.Helper()
	if got.Alias != want.Alias || got.Repository != want.Repository || got.Reference != want.Reference {
		t.Fatalf("suggestion request = %#v, want %#v", got, want)
	}
}

type manifestErrorResponse struct {
	Errors []struct {
		Code    distribution.ErrorCode             `json:"code"`
		Message string                             `json:"message"`
		Detail  distribution.ManifestUnknownDetail `json:"detail"`
	} `json:"errors"`
}

var _ suggestion.ManifestService = (*recordingSuggestionService)(nil)
