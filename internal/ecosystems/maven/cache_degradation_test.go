package maven

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestCachedBackendErrorFallsBackToMiss(t *testing.T) {
	service, metadata := newCacheDegradationService(t)
	if err := metadata.Close(); err != nil {
		t.Fatalf("close metadata: %v", err)
	}

	entry, ok, err := service.cached(context.Background(), Route{
		Alias:      "central",
		Repository: "com/acme/demo/1.0",
		Reference:  "demo-1.0.jar",
	})
	if err != nil {
		t.Fatalf("cached returned error: %v", err)
	}
	if ok || entry.body != nil {
		t.Fatalf("cached = %#v, %t, want miss", entry, ok)
	}
}

func TestResponseFromFetchedServesUpstreamWhenCacheWriteFails(t *testing.T) {
	service, metadata := newCacheDegradationService(t)
	if err := metadata.Close(); err != nil {
		t.Fatalf("close metadata: %v", err)
	}

	response, err := service.responseFromFetched(context.Background(), Request{Method: http.MethodGet}, Route{
		Alias:      "central",
		Repository: "com/acme/demo/1.0",
		Reference:  "demo-1.0.jar",
	}, &upstreamFetch{
		status:  http.StatusOK,
		headers: http.Header{"Content-Type": []string{"application/java-archive"}},
		body:    io.NopCloser(strings.NewReader("healthy upstream")),
	})
	if err != nil {
		t.Fatalf("responseFromFetched returned error: %v", err)
	}
	if response == nil || response.Status != http.StatusOK {
		t.Fatalf("response = %#v, want healthy upstream response", response)
	}
	if response.Cache != cacheMiss {
		t.Fatalf("cache = %q, want %q", response.Cache, cacheMiss)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("close response body: %v", closeErr)
	}
	if string(body) != "healthy upstream" {
		t.Fatalf("response body = %q, want healthy upstream", string(body))
	}
}

func TestResponseFromFetchedCacheWriteHonorsCanceledContext(t *testing.T) {
	service, _ := newCacheDegradationService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	response, err := service.responseFromFetched(ctx, Request{Method: http.MethodGet}, Route{
		Alias:      "central",
		Repository: "com/acme/demo/1.0",
		Reference:  "demo-1.0.jar",
	}, &upstreamFetch{
		status:  http.StatusOK,
		headers: http.Header{"Content-Type": []string{"application/java-archive"}},
		body:    io.NopCloser(strings.NewReader("healthy upstream")),
	})
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cache write took %s after context cancellation", elapsed)
	}
	if err != nil {
		t.Fatalf("responseFromFetched returned error: %v", err)
	}
	if response == nil {
		t.Fatal("response is nil")
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("close response body: %v", closeErr)
	}
	if string(body) != "healthy upstream" {
		t.Fatalf("response body = %q, want healthy upstream", string(body))
	}
}

func newCacheDegradationService(t *testing.T) (*Service, meta.Store) {
	t.Helper()
	metadata, err := meta.OpenSQLiteWithOptions(context.Background(), meta.DBOptions{
		Path: filepath.Join(t.TempDir(), "regimux.db"),
	})
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	t.Cleanup(func() { _ = metadata.Close() })
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		_ = metadata.Close()
		t.Fatalf("open objects: %v", err)
	}
	cache := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata,
		Objects:  objects,
	})
	service := &Service{cache: cache}
	return service, metadata
}
