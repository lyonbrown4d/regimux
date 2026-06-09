package container_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/suggestion"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

func startAPIServer(t *testing.T, endpoints ...httpx.Endpoint) string {
	t.Helper()
	all := collectionlist.NewList[httpx.Endpoint]()
	for _, endpoint := range endpoints {
		all.Add(endpoint)
	}
	return startAPIServerWithOptions(t, api.Options{Endpoints: all})
}

func startAPIServerWithOptions(t *testing.T, opts api.Options) string {
	t.Helper()

	addr := freeTCPAddr(t)
	opts.Listen = addr
	opts.Middleware.Healthcheck.Enabled = true
	opts.Middleware.Healthcheck.LivenessPath = "/livez"
	opts.Middleware.Healthcheck.ReadinessPath = "/readyz"
	endpoints := collectionlist.NewList[httpx.Endpoint]()
	if opts.Endpoints != nil {
		opts.Endpoints.Range(func(_ int, endpoint httpx.Endpoint) bool {
			endpoints.Add(endpoint)
			return true
		})
	}
	opts.Endpoints = endpoints
	server := api.NewServer(opts)
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("start api server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Stop(ctx); err != nil {
			t.Fatalf("stop api server: %v", err)
		}
	})

	baseURL := "http://" + addr
	waitForHTTP(t, baseURL+"/livez")
	return baseURL
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate tcp listener: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close tcp listener: %v", err)
	}
	return addr
}

func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpGetWithClient(client, url)
		if err == nil {
			readHTTPResponse(t, resp)
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server did not become ready at %s", url)
}

func httpGet(t *testing.T, url string) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := httpGetWithClient(client, url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	return resp
}

func httpGetWithClient(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, oops.Wrapf(err, "build test request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, oops.Wrapf(err, "send test request")
	}
	return resp, nil
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

func readHTTPResponse(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		t.Fatalf("read response body: %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("close response body: %v", closeErr)
	}
	return body
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
