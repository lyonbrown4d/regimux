package golang_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/golang"
	"github.com/lyonbrown4d/regimux/internal/testkit"
)

func TestServiceCoalesces128ConcurrentVersionedGoProxyMisses(t *testing.T) {
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
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeResponse(t, w, "module github.com/acme/lib\n")
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL)
	t.Cleanup(releaseFill)
	run := testkit.StartConcurrent(clients, func() (*golang.Response, error) {
		return service.Get(ctx, golang.Request{Alias: "default", Tail: "github.com/acme/lib/@v/v1.2.3.mod"})
	}).WithTimeout(30 * time.Second)
	testkit.WaitForSignal(t, started)
	releaseFill()

	run.RequireOneMiss(t, cacheMiss, cacheHit, func(resp *golang.Response) string {
		assertBody(t, resp, "module github.com/acme/lib\n")
		return resp.Cache
	})
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

func TestServiceAllows128DistinctGoProxyMissesToOverlap(t *testing.T) {
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
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		writeResponse(t, w, goProxyBody(goProxyVersionID(r.URL.Path)))
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL)
	t.Cleanup(releaseFill)
	var next atomic.Int64
	run := testkit.StartConcurrent(clients, func() (*golang.Response, error) {
		id := next.Add(1)
		return service.Get(ctx, golang.Request{
			Alias: "default",
			Tail:  fmt.Sprintf("github.com/acme/lib/@v/v1.%d.0.mod", id),
		})
	}).WithTimeout(30 * time.Second)
	waitForGoProxyRequests(t, requests, clients)
	releaseFill()

	responses := run.Wait(t)
	assertGoProxyNotFoundResponses(t, responses, clients)
}

func TestServiceStores128DistinctGoProxyMissesAndServesCachedBodies(t *testing.T) {
	ctx := t.Context()
	const clients = 128
	requests := make(chan string, clients)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFill := func() { releaseOnce.Do(func() { close(release) }) }

	upstream := newSuccessfulGoProxyServer(t, requests, release)
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL)
	t.Cleanup(releaseFill)
	allRequests := goProxyRequests(clients)
	run := startGoProxyRequests(ctx, service, allRequests)
	waitForGoProxyRequests(t, requests, clients)
	releaseFill()

	responses := run.Wait(t)
	assertGoProxySuccessfulMisses(t, responses, allRequests)
	assertGoProxyCacheHits(ctx, t, service, allRequests)
	assertGoProxyNoOriginRequests(t, requests)
}

func newSuccessfulGoProxyServer(t *testing.T, requests chan<- string, release <-chan struct{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeResponse(t, w, goProxyBody(goProxyVersionID(r.URL.Path)))
	}))
}

func goProxyRequests(count int) []golang.Request {
	requests := make([]golang.Request, count)
	for i := range count {
		requests[i] = golang.Request{Alias: "default", Tail: fmt.Sprintf("github.com/acme/lib/@v/v1.%d.0.mod", i+1)}
	}
	return requests
}

func startGoProxyRequests(ctx context.Context, service *golang.Service, requests []golang.Request) *testkit.ConcurrentRun[*golang.Response] {
	var index atomic.Int64
	return testkit.StartConcurrent(len(requests), func() (*golang.Response, error) {
		i := index.Add(1) - 1
		return service.Get(ctx, requests[i])
	}).WithTimeout(60 * time.Second)
}

func assertGoProxyNotFoundResponses(t *testing.T, responses []*golang.Response, count int) {
	t.Helper()
	wantBodies := goProxyBodies(count)
	for _, resp := range responses {
		if resp.Status != http.StatusNotFound {
			t.Fatalf("distinct-key response status = %d, want %d", resp.Status, http.StatusNotFound)
		}
		body := responseBody(t, resp)
		if _, ok := wantBodies[body]; !ok {
			t.Fatalf("unexpected distinct-key response body %q", body)
		}
		delete(wantBodies, body)
	}
	if len(wantBodies) != 0 {
		t.Fatalf("404 wave missed %d distinct bodies", len(wantBodies))
	}
}

func assertGoProxySuccessfulMisses(t *testing.T, responses []*golang.Response, requests []golang.Request) {
	t.Helper()
	count := len(requests)
	if len(responses) != count {
		t.Fatalf("first-wave responses = %d, want %d", len(responses), count)
	}
	wantBodies := goProxyBodies(count)
	for i, resp := range responses {
		if resp.Status != http.StatusOK || resp.Cache != cacheMiss {
			t.Fatalf("first-wave response %d status/cache = %d/%q, want %d/%q", i, resp.Status, resp.Cache, http.StatusOK, cacheMiss)
		}
		body := responseBody(t, resp)
		if _, ok := wantBodies[body]; !ok {
			t.Fatalf("unexpected first-wave body %q", body)
		}
		delete(wantBodies, body)
	}
	if len(wantBodies) != 0 {
		t.Fatalf("first wave missed %d distinct upstream bodies", len(wantBodies))
	}
}

func assertGoProxyCacheHits(ctx context.Context, t *testing.T, service *golang.Service, requests []golang.Request) {
	t.Helper()
	for i, request := range requests {
		resp, err := service.Get(ctx, request)
		if err != nil {
			t.Fatalf("cache verification request %d: %v", i, err)
		}
		if resp.Status != http.StatusOK || resp.Cache != cacheHit {
			t.Fatalf("cache response %d status/cache = %d/%q, want %d/%q", i, resp.Status, resp.Cache, http.StatusOK, cacheHit)
		}
		assertBody(t, resp, goProxyBody(i+1))
	}
}

func assertGoProxyNoOriginRequests(t *testing.T, requests <-chan string) {
	t.Helper()
	if got := len(requests); got != 0 {
		t.Fatalf("unexpected upstream requests after cache hits: %d", got)
	}
}

func goProxyBodies(count int) map[string]struct{} {
	bodies := make(map[string]struct{}, count)
	for id := range count {
		bodies[goProxyBody(id+1)] = struct{}{}
	}
	return bodies
}

func goProxyBody(id int) string {
	return fmt.Sprintf("module github.com/acme/lib\n// version %d\n", id)
}

func goProxyVersionID(path string) int {
	version := strings.TrimPrefix(path, "/github.com/acme/lib/@v/v1.")
	version = strings.TrimSuffix(version, ".0.mod")
	id, err := strconv.Atoi(version)
	if err != nil {
		return 0
	}
	return id
}

func waitForGoProxyRequests(t *testing.T, requests <-chan string, count int) {
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
