//revive:disable:file-length-limit Go proxy scenarios stay grouped to share fixtures and route assertions.
package golang_test

import (
	"context"
	"github.com/lyonbrown4d/regimux/internal/testkit"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/golang"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const (
	cacheHit   = "hit"
	cacheMiss  = "miss"
	cacheStale = "stale"
)

func TestServiceCachesVersionedGoProxyFile(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/github.com/acme/lib/@v/v1.2.3.mod" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeResponse(t, w, "module github.com/acme/lib\n")
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL)
	first, err := service.Get(ctx, golang.Request{
		Alias: "default",
		Tail:  "github.com/acme/lib/@v/v1.2.3.mod",
	})
	requireNoError(t, "first get", err)
	assertBody(t, first, "module github.com/acme/lib\n")
	if first.Cache != cacheMiss {
		t.Fatalf("first cache = %q, want %q", first.Cache, cacheMiss)
	}

	second, err := service.Get(ctx, golang.Request{
		Alias: "default",
		Tail:  "github.com/acme/lib/@v/v1.2.3.mod",
	})
	requireNoError(t, "second get", err)
	assertBody(t, second, "module github.com/acme/lib\n")
	if second.Cache != cacheHit {
		t.Fatalf("second cache = %q, want %q", second.Cache, cacheHit)
	}
	if requests != 1 {
		t.Fatalf("upstream requests = %d, want 1", requests)
	}
}

func TestServiceCoalescesConcurrentVersionedGoProxyMiss(t *testing.T) {
	ctx := context.Background()
	var requests atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		if r.URL.Path != "/github.com/acme/lib/@v/v1.2.3.mod" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		<-release
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeResponse(t, w, "module github.com/acme/lib\n")
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL)
	const clients = 8
	run := testkit.StartConcurrent(clients, func() (*golang.Response, error) {
		return service.Get(ctx, golang.Request{Alias: "default", Tail: "github.com/acme/lib/@v/v1.2.3.mod"})
	})
	testkit.WaitForSignal(t, started)
	releaseOnce.Do(func() { close(release) })

	responses := run.Wait(t)
	testkit.RequireOneMiss(t, responses, cacheMiss, cacheHit, func(resp *golang.Response) string {
		assertBody(t, resp, "module github.com/acme/lib\n")
		return resp.Cache
	})
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}
func TestServiceCachesRootGoProxyFile(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/github.com/acme/lib/@v/v1.2.3.info" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, `{"Version":"v1.2.3"}`)
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL)
	first, err := service.Get(ctx, golang.Request{
		Tail: "github.com/acme/lib/@v/v1.2.3.info",
	})
	requireNoError(t, "first root get", err)
	assertBody(t, first, `{"Version":"v1.2.3"}`)
	if first.Cache != cacheMiss {
		t.Fatalf("first cache = %q, want %q", first.Cache, cacheMiss)
	}

	second, err := service.Get(ctx, golang.Request{
		Tail: "github.com/acme/lib/@v/v1.2.3.info",
	})
	requireNoError(t, "second root get", err)
	assertBody(t, second, `{"Version":"v1.2.3"}`)
	if second.Cache != cacheHit {
		t.Fatalf("second cache = %q, want %q", second.Cache, cacheHit)
	}
	if requests != 1 {
		t.Fatalf("upstream requests = %d, want 1", requests)
	}
}

func TestServicePersistsModuleZipAfterFullDownload(t *testing.T) {
	ctx := context.Background()
	const body = "PK\x05\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/github.com/acme/lib/@v/v1.2.3.zip" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/zip")
		writeResponse(t, w, body)
	}))
	t.Cleanup(upstream.Close)

	service, metadata, objects := newTestServiceWithStores(ctx, t, map[string]config.DependencyUpstreamConfig{
		"default": {Registry: upstream.URL},
	})
	resp, err := service.Get(ctx, golang.Request{
		Alias: "default",
		Tail:  "github.com/acme/lib/@v/v1.2.3.zip",
	})
	requireNoError(t, "module zip get", err)
	assertBody(t, resp, body)
	if resp.Cache != cacheMiss {
		t.Fatalf("cache = %q, want %q", resp.Cache, cacheMiss)
	}

	assertStoredArtifact(ctx, t, metadata, objects, meta.TagKey{
		Alias:      "default",
		Repository: "github.com/acme/lib",
		Reference:  "@v/v1.2.3.zip",
	}, body, "application/zip")
}

func TestServiceServesExpiredLatestStaleWithoutRefreshingInline(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/github.com/acme/lib/@latest" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, `{"Version":"v1.2.`+strconv.Itoa(requests)+`"}`)
	}))
	t.Cleanup(upstream.Close)

	service, metadata := newTestServiceWithMetadata(ctx, t, map[string]config.DependencyUpstreamConfig{
		"default": {Registry: upstream.URL},
	})
	req := golang.Request{Alias: "default", Tail: "github.com/acme/lib/@latest"}

	first, err := service.Get(ctx, req)
	requireNoError(t, "first latest get", err)
	firstBody := responseBody(t, first)
	if first.Cache != cacheMiss {
		t.Fatalf("first cache = %q, want %q", first.Cache, cacheMiss)
	}

	expireArtifactMetadata(ctx, t, metadata, "default", "github.com/acme/lib", "@latest")
	second, err := service.Get(ctx, req)
	requireNoError(t, "second latest get", err)
	secondBody := responseBody(t, second)
	if second.Cache != cacheStale {
		t.Fatalf("second cache = %q, want %q", second.Cache, cacheStale)
	}
	if secondBody != firstBody {
		t.Fatalf("second body = %q, want stale body %q", secondBody, firstBody)
	}
	if requests != 1 {
		t.Fatalf("upstream requests after stale hit = %d, want 1", requests)
	}
}

func TestServiceRootGoProxyFallsBackAcrossGoUpstreams(t *testing.T) {
	ctx := context.Background()
	primaryRequests := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryRequests++
		if r.URL.Path != "/github.com/acme/lib/@v/v1.2.3.mod" {
			t.Fatalf("primary path = %s", r.URL.Path)
		}
		http.Error(w, "missing", http.StatusNotFound)
	}))
	t.Cleanup(primary.Close)

	backupRequests := 0
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupRequests++
		if r.URL.Path != "/github.com/acme/lib/@v/v1.2.3.mod" {
			t.Fatalf("backup path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeResponse(t, w, "module github.com/acme/lib\n")
	}))
	t.Cleanup(backup.Close)

	service := newTestServiceWithUpstreams(ctx, t, map[string]config.DependencyUpstreamConfig{
		"backup":  {Registry: backup.URL},
		"default": {Registry: primary.URL},
	})
	resp, err := service.Get(ctx, golang.Request{
		Tail: "github.com/acme/lib/@v/v1.2.3.mod",
	})
	requireNoError(t, "root fallback get", err)
	assertBody(t, resp, "module github.com/acme/lib\n")
	if resp.Cache != cacheMiss {
		t.Fatalf("cache = %q, want %q", resp.Cache, cacheMiss)
	}
	if primaryRequests != 1 {
		t.Fatalf("primary requests = %d, want 1", primaryRequests)
	}
	if backupRequests != 1 {
		t.Fatalf("backup requests = %d, want 1", backupRequests)
	}
}

func TestServicePassesThroughNotFound(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL)
	resp, err := service.Get(ctx, golang.Request{
		Alias: "default",
		Tail:  "github.com/acme/missing/@v/v1.0.0.mod",
	})
	requireNoError(t, "get missing", err)
	if resp.Status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.Status, http.StatusNotFound)
	}
	assertBody(t, resp, "not found\n")
}

func TestServiceDoesNotStoreHeadMiss(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodHead {
			t.Fatalf("method = %s, want HEAD", r.Method)
		}
		w.Header().Set("Content-Length", "128")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL)
	for range 2 {
		resp, err := service.Get(ctx, golang.Request{
			Alias:  "default",
			Tail:   "github.com/acme/lib/@v/v1.2.3.zip",
			Method: http.MethodHead,
		})
		requireNoError(t, "head get", err)
		if resp.Status != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.Status, http.StatusOK)
		}
		if resp.Cache != cacheMiss {
			t.Fatalf("cache = %q, want %q", resp.Cache, cacheMiss)
		}
	}
	if requests != 2 {
		t.Fatalf("upstream requests = %d, want 2", requests)
	}
}
