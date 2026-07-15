//revive:disable:file-length-limit Maven proxy scenarios stay grouped to share fixtures and repository-layout assertions.
package maven_test

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
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const (
	cacheHit   = "hit"
	cacheMiss  = "miss"
	cacheStale = "stale"
)

func TestServiceCachesReleaseArtifactByPath(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/com/acme/demo/1.2.3/demo-1.2.3.jar" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/java-archive")
		writeResponse(t, w, "jar bytes")
	}))
	t.Cleanup(upstream.Close)

	service, metadata := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: upstream.URL},
	})
	first, err := service.Get(ctx, maven.Request{
		Alias: "central",
		Tail:  "com/acme/demo/1.2.3/demo-1.2.3.jar",
	})
	requireNoError(t, "first get", err)
	assertBody(t, first, "jar bytes")
	if first.Cache != cacheMiss {
		t.Fatalf("first cache = %q, want %q", first.Cache, cacheMiss)
	}

	second, err := service.Get(ctx, maven.Request{
		Alias: "central",
		Tail:  "com/acme/demo/1.2.3/demo-1.2.3.jar",
	})
	requireNoError(t, "second get", err)
	assertBody(t, second, "jar bytes")
	if second.Cache != cacheHit {
		t.Fatalf("second cache = %q, want %q", second.Cache, cacheHit)
	}
	if requests != 1 {
		t.Fatalf("upstream requests = %d, want 1", requests)
	}

	tag, ok, err := metadata.Tag(ctx, meta.TagKey{
		Alias:      "central",
		Repository: "com/acme/demo/1.2.3",
		Reference:  "demo-1.2.3.jar",
	})
	requireNoError(t, "lookup tag", err)
	if !ok {
		t.Fatal("release artifact cache metadata was not stored")
	}
	if !tag.ExpiresAt.IsZero() {
		t.Fatalf("release artifact expires_at = %s, want zero", tag.ExpiresAt)
	}
}

func TestServiceCoalescesConcurrentReleaseArtifactMiss(t *testing.T) {
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
		if r.URL.Path != "/com/acme/demo/1.2.3/demo-1.2.3.jar" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		<-release
		w.Header().Set("Content-Type", "application/java-archive")
		writeResponse(t, w, "jar bytes")
	}))
	t.Cleanup(upstream.Close)

	service, _ := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: upstream.URL},
	})
	const clients = 8
	run := testkit.StartConcurrent(clients, func() (*maven.Response, error) {
		return service.Get(ctx, maven.Request{Alias: "central", Tail: "com/acme/demo/1.2.3/demo-1.2.3.jar"})
	})
	testkit.WaitForSignal(t, started)
	releaseOnce.Do(func() { close(release) })

	responses := run.Wait(t)
	testkit.RequireOneMiss(t, responses, cacheMiss, cacheHit, func(resp *maven.Response) string {
		assertBody(t, resp, "jar bytes")
		return resp.Cache
	})
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}
func TestServicePersistsArtifactAfterFullDownload(t *testing.T) {
	ctx := context.Background()
	const body = "jar bytes"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/com/acme/demo/1.2.3/demo-1.2.3.jar" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/java-archive")
		writeResponse(t, w, body)
	}))
	t.Cleanup(upstream.Close)

	service, metadata, objects := newTestServiceWithStores(ctx, t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: upstream.URL},
	})
	resp, err := service.Get(ctx, maven.Request{
		Alias: "central",
		Tail:  "com/acme/demo/1.2.3/demo-1.2.3.jar",
	})
	requireNoError(t, "artifact get", err)
	assertBody(t, resp, body)
	if resp.Cache != cacheMiss {
		t.Fatalf("cache = %q, want %q", resp.Cache, cacheMiss)
	}

	assertStoredArtifact(ctx, t, metadata, objects, meta.TagKey{
		Alias:      "central",
		Repository: "com/acme/demo/1.2.3",
		Reference:  "demo-1.2.3.jar",
	}, body, "application/java-archive")
}

func TestServiceStoresShortTTLForMetadataAndSnapshots(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/com/acme/demo/maven-metadata.xml":
			w.Header().Set("Content-Type", "application/xml")
			writeResponse(t, w, "<metadata/>")
		case "/com/acme/demo/1.3-SNAPSHOT/demo-1.3-20260603.010203-1.jar":
			w.Header().Set("Content-Type", "application/java-archive")
			writeResponse(t, w, "snapshot jar")
		default:
			t.Fatalf("unexpected upstream path = %s", r.URL.Path)
		}
	}))
	t.Cleanup(upstream.Close)

	service, metadata := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: upstream.URL},
	})
	metadataResp, err := service.Get(ctx, maven.Request{
		Alias: "central",
		Tail:  "com/acme/demo/maven-metadata.xml",
	})
	requireNoError(t, "metadata get", err)
	assertBody(t, metadataResp, "<metadata/>")

	snapshotResp, err := service.Get(ctx, maven.Request{
		Alias: "central",
		Tail:  "com/acme/demo/1.3-SNAPSHOT/demo-1.3-20260603.010203-1.jar",
	})
	requireNoError(t, "snapshot get", err)
	assertBody(t, snapshotResp, "snapshot jar")

	assertTagExpires(ctx, t, metadata, meta.TagKey{
		Alias:      "central",
		Repository: "com/acme/demo",
		Reference:  "maven-metadata.xml",
	})
	assertTagExpires(ctx, t, metadata, meta.TagKey{
		Alias:      "central",
		Repository: "com/acme/demo/1.3-SNAPSHOT",
		Reference:  "demo-1.3-20260603.010203-1.jar",
	})
}

func TestServiceServesExpiredMetadataStaleWithoutRefreshingInline(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/com/acme/demo/maven-metadata.xml" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/xml")
		writeResponse(t, w, "<metadata><version>"+strconv.Itoa(requests)+"</version></metadata>")
	}))
	t.Cleanup(upstream.Close)

	service, metadata := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: upstream.URL},
	})
	req := maven.Request{Alias: "central", Tail: "com/acme/demo/maven-metadata.xml"}

	first, err := service.Get(ctx, req)
	requireNoError(t, "first metadata get", err)
	firstBody := responseBody(t, first)
	if first.Cache != cacheMiss {
		t.Fatalf("first cache = %q, want %q", first.Cache, cacheMiss)
	}

	expireArtifactMetadata(ctx, t, metadata, "central", "com/acme/demo", "maven-metadata.xml")
	second, err := service.Get(ctx, req)
	requireNoError(t, "second metadata get", err)
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

func TestServiceUsesMirrorsWhenRegistryIsEmpty(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/org/example/lib/2.0/lib-2.0.pom" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/xml")
		writeResponse(t, w, "<project/>")
	}))
	t.Cleanup(upstream.Close)

	service, _ := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{
		"mirror": {Mirrors: []string{upstream.URL}},
	})
	resp, err := service.Get(ctx, maven.Request{
		Alias: "mirror",
		Tail:  "org/example/lib/2.0/lib-2.0.pom",
	})
	requireNoError(t, "mirror get", err)
	assertBody(t, resp, "<project/>")
}

func TestServicePassesThroughNotFound(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	service, _ := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: upstream.URL},
	})
	resp, err := service.Get(ctx, maven.Request{
		Alias: "central",
		Tail:  "com/acme/missing/1.0/missing-1.0.jar",
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

	service, _ := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: upstream.URL},
	})
	for range 2 {
		resp, err := service.Get(ctx, maven.Request{
			Alias:  "central",
			Tail:   "com/acme/demo/1.2.3/demo-1.2.3.jar",
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
