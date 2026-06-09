package golang_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/golang"
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

func TestServiceRejectsNonGoUpstream(t *testing.T) {
	ctx := context.Background()
	service := golang.NewService(golang.ServiceDependencies{
		Config: config.Config{
			Container: config.ContainerConfig{
				"hub": {Registry: "https://registry-1.docker.io"},
			},
		},
	})
	_, err := service.Get(ctx, golang.Request{
		Alias: "default",
		Tail:  "github.com/acme/lib/@v/v1.2.3.mod",
	})
	if err == nil {
		t.Fatal("expected non-go upstream error")
	}
}
