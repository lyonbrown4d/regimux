package maven_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const (
	cacheHit  = "hit"
	cacheMiss = "miss"
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

func TestServiceRejectsNonMavenUpstream(t *testing.T) {
	ctx := context.Background()
	service := maven.NewService(maven.ServiceDependencies{
		Config: config.Config{
			Go: config.DependencyEcosystemConfig{
				"default": {Registry: "https://proxy.golang.org"},
			},
		},
	})
	_, err := service.Get(ctx, maven.Request{
		Alias: "default",
		Tail:  "com/acme/demo/1.2.3/demo-1.2.3.jar",
	})
	if err == nil {
		t.Fatal("expected non-maven upstream error")
	}
}

func newTestService(ctx context.Context, t *testing.T, upstreams map[string]config.DependencyUpstreamConfig) (*maven.Service, meta.Store) {
	t.Helper()
	db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", db.Close())
	})
	objects, err := object.NewMemory("maven-test")
	requireNoError(t, "open objects", err)
	return maven.NewService(maven.ServiceDependencies{
		Config: config.Config{
			Maven: upstreams,
		},
		Metadata: db,
		Objects:  objects,
	}), db
}

func assertTagExpires(ctx context.Context, t *testing.T, metadata meta.Store, key meta.TagKey) {
	t.Helper()
	tag, ok, err := metadata.Tag(ctx, key)
	requireNoError(t, "lookup tag", err)
	if !ok {
		t.Fatalf("tag %s was not stored", key.String())
	}
	if tag.ExpiresAt.IsZero() {
		t.Fatalf("tag %s expires_at is zero, want short TTL", key.String())
	}
}

func assertBody(t *testing.T, resp *maven.Response, want string) {
	t.Helper()
	if resp == nil || resp.Body == nil {
		t.Fatalf("response body is empty")
	}
	defer closeBody(t, resp.Body)
	body, err := io.ReadAll(resp.Body)
	requireNoError(t, "read body", err)
	if string(body) != want {
		t.Fatalf("body = %q, want %q", string(body), want)
	}
}

func writeResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func closeBody(t *testing.T, body io.Closer) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
}

func requireNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}
