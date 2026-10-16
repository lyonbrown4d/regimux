package npm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/npm"
	"github.com/lyonbrown4d/regimux/internal/testkit"
)

func TestServiceCoalesces128ConcurrentMetadataMisses(t *testing.T) {
	ctx := t.Context()
	const clients = 128
	var requests atomic.Int64
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFill := func() { releaseOnce.Do(func() { close(release) }) }

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			started <- struct{}{}
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeNPMMetadata(t, w, "left-pad")
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL, 5*time.Minute)
	t.Cleanup(releaseFill)
	run := testkit.StartConcurrent(clients, func() (*npm.Response, error) {
		return service.Get(ctx, npm.Request{Alias: "npmjs", Tail: "left-pad"})
	}).WithTimeout(30 * time.Second)
	testkit.WaitForSignal(t, started)
	releaseFill()

	run.RequireOneMiss(t, cacheMiss, cacheHit, func(resp *npm.Response) string {
		if body := readBody(t, resp); body != `{"name":"left-pad","versions":{}}` {
			t.Fatalf("metadata body = %q", body)
		}
		return resp.Cache
	})
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

func TestServiceAllows128DistinctNPMMetadataMissesToOverlap(t *testing.T) {
	ctx := t.Context()
	const clients = 128
	requests := make(chan string, clients)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFill := func() { releaseOnce.Do(func() { close(release) }) }

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeNPMNotFound(t, w, npmPackageName(r.URL.Path))
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL, 5*time.Minute)
	t.Cleanup(releaseFill)
	var next atomic.Int64
	run := testkit.StartConcurrent(clients, func() (*npm.Response, error) {
		id := next.Add(1)
		return service.Get(ctx, npm.Request{Alias: "npmjs", Tail: fmt.Sprintf("pkg-%d", id)})
	}).WithTimeout(30 * time.Second)
	waitForNPMProxyRequests(t, requests, clients)
	releaseFill()

	responses := run.Wait(t)
	assertNPMNotFoundResponses(t, responses, clients)
}

func TestServiceStores128DistinctNPMMetadataMissesAndServesCachedBodies(t *testing.T) {
	ctx := t.Context()
	const clients = 128
	requests := make(chan string, clients)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFill := func() { releaseOnce.Do(func() { close(release) }) }

	upstream := newSuccessfulNPMServer(t, requests, release)
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL, 5*time.Minute)
	t.Cleanup(releaseFill)
	allRequests := npmRequests(clients)
	run := startNPMRequests(ctx, service, allRequests)
	waitForNPMProxyRequests(t, requests, clients)
	releaseFill()

	responses := run.Wait(t)
	assertNPMSuccessfulMisses(t, responses, allRequests)
	assertNPMCacheHits(ctx, t, service, allRequests)
	assertNPMNoOriginRequests(t, requests)
}

func newSuccessfulNPMServer(t *testing.T, requests chan<- string, release <-chan struct{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeNPMMetadata(t, w, npmPackageName(r.URL.Path))
	}))
}

func npmRequests(count int) []npm.Request {
	requests := make([]npm.Request, count)
	for i := range count {
		requests[i] = npm.Request{Alias: "npmjs", Tail: fmt.Sprintf("pkg-%d", i+1)}
	}
	return requests
}

func startNPMRequests(ctx context.Context, service *npm.Service, requests []npm.Request) *testkit.ConcurrentRun[*npm.Response] {
	var index atomic.Int64
	return testkit.StartConcurrent(len(requests), func() (*npm.Response, error) {
		i := index.Add(1) - 1
		return service.Get(ctx, requests[i])
	}).WithTimeout(60 * time.Second)
}

func assertNPMNotFoundResponses(t *testing.T, responses []*npm.Response, count int) {
	t.Helper()
	wantBodies := npmNotFoundBodies(count)
	for _, resp := range responses {
		if resp.Status != http.StatusNotFound {
			t.Fatalf("distinct-key response status = %d, want %d", resp.Status, http.StatusNotFound)
		}
		body := readBody(t, resp)
		if _, ok := wantBodies[body]; !ok {
			t.Fatalf("unexpected distinct-key metadata body %q", body)
		}
		delete(wantBodies, body)
	}
	if len(wantBodies) != 0 {
		t.Fatalf("404 wave missed %d distinct metadata bodies", len(wantBodies))
	}
}

func assertNPMSuccessfulMisses(t *testing.T, responses []*npm.Response, requests []npm.Request) {
	t.Helper()
	if len(responses) != len(requests) {
		t.Fatalf("first-wave responses = %d, want %d", len(responses), len(requests))
	}
	wantBodies := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		wantBodies[npmMetadataBody(request.Tail)] = struct{}{}
	}
	for i, resp := range responses {
		if resp.Status != http.StatusOK || resp.Cache != cacheMiss {
			t.Fatalf("first-wave response %d status/cache = %d/%q, want %d/%q", i, resp.Status, resp.Cache, http.StatusOK, cacheMiss)
		}
		body := readBody(t, resp)
		if _, ok := wantBodies[body]; !ok {
			t.Fatalf("unexpected first-wave metadata body %q", body)
		}
		delete(wantBodies, body)
	}
	if len(wantBodies) != 0 {
		t.Fatalf("first wave missed %d distinct metadata bodies", len(wantBodies))
	}
}

func assertNPMCacheHits(ctx context.Context, t *testing.T, service *npm.Service, requests []npm.Request) {
	t.Helper()
	for i, request := range requests {
		resp, err := service.Get(ctx, request)
		if err != nil {
			t.Fatalf("cache verification request %d: %v", i, err)
		}
		if resp.Status != http.StatusOK || resp.Cache != cacheHit {
			t.Fatalf("cache response %d status/cache = %d/%q, want %d/%q", i, resp.Status, resp.Cache, http.StatusOK, cacheHit)
		}
		if body := readBody(t, resp); body != npmMetadataBody(request.Tail) {
			t.Fatalf("cache response %d metadata body = %q, want %q", i, body, npmMetadataBody(request.Tail))
		}
	}
}

func assertNPMNoOriginRequests(t *testing.T, requests <-chan string) {
	t.Helper()
	if got := len(requests); got != 0 {
		t.Fatalf("unexpected upstream requests after cache hits: %d", got)
	}
}

func npmPackageName(path string) string {
	return strings.TrimPrefix(path, "/")
}

func writeNPMMetadata(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	body, err := json.Marshal(struct {
		Name     string         `json:"name"`
		Versions map[string]any `json:"versions"`
	}{Name: name, Versions: map[string]any{}})
	if err != nil {
		t.Fatalf("marshal npm metadata: %v", err)
	}
	writeResponse(t, w, string(body))
}

func writeNPMNotFound(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	body, err := json.Marshal(struct {
		Error string `json:"error"`
		Name  string `json:"name"`
	}{Error: "not_found", Name: name})
	if err != nil {
		t.Fatalf("marshal npm not-found metadata: %v", err)
	}
	writeResponse(t, w, string(body))
}

func npmMetadataBody(name string) string {
	body, err := json.Marshal(struct {
		Name     string         `json:"name"`
		Versions map[string]any `json:"versions"`
	}{Name: name, Versions: map[string]any{}})
	if err != nil {
		return ""
	}
	return string(body)
}

func npmNotFoundBodies(count int) map[string]struct{} {
	bodies := make(map[string]struct{}, count)
	for id := range count {
		bodies[marshalNPMNotFound(fmt.Sprintf("pkg-%d", id+1))] = struct{}{}
	}
	return bodies
}

func marshalNPMNotFound(name string) string {
	body, err := json.Marshal(struct {
		Error string `json:"error"`
		Name  string `json:"name"`
	}{Error: "not_found", Name: name})
	if err != nil {
		return ""
	}
	return string(body)
}

func waitForNPMProxyRequests(t *testing.T, requests <-chan string, count int) {
	t.Helper()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	seen := make(map[string]struct{}, count)
	for len(seen) < count {
		select {
		case path := <-requests:
			seen[path] = struct{}{}
		case <-timer.C:
			t.Fatalf("upstream received %d distinct-key requests, want %d", len(seen), count)
		}
	}
}
