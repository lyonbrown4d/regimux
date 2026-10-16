//nolint:testpackage // Cache degradation tests exercise private service paths.
package maven

import (
	"context"
	"errors"
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
	if err == nil {
		if response != nil && response.Body != nil {
			closeReadCloser(response.Body, nil, "close unexpected response")
		}
		t.Fatal("responseFromFetched succeeded after request cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("responseFromFetched error = %v, want context canceled", err)
	}
}

func TestResponseFromFetchedRejectsInvalidPOMWhenCacheUnavailable(t *testing.T) {
	service := &Service{cache: artifactcache.New(artifactcache.Dependencies{})}
	response, err := service.responseFromFetched(context.Background(), Request{Method: http.MethodGet}, Route{
		Alias: "central", Repository: "com/acme/demo/1.0", Reference: "demo-1.0.pom",
	}, &upstreamFetch{
		status:  http.StatusOK,
		headers: http.Header{"Content-Type": []string{"application/xml"}},
		body:    io.NopCloser(strings.NewReader("<definitely-not-a-pom>")),
	})
	if response != nil && response.Body != nil {
		closeReadCloser(response.Body, nil, "close invalid response")
	}
	if err == nil {
		t.Fatal("invalid POM was served when cache dependencies were unavailable")
	}
}

//nolint:cyclop,gocognit,gocyclo // The test wires the real cache, metadata, and context-bound object reader.
func TestCachedResponseKeepsReadContextUntilBodyClose(t *testing.T) {
	ctx := context.Background()
	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Errorf("close metadata: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open objects: %v", err)
	}
	key := artifactcache.Key{Alias: "central", Repository: "com/acme/demo/1.0", Reference: "demo-1.0.jar"}
	seed := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})
	seeded, err := seed.Put(ctx, artifactcache.PutRequest{Key: key, Body: strings.NewReader("cached artifact")})
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	if seeded.Body != nil {
		if closeErr := seeded.Body.Close(); closeErr != nil {
			t.Fatalf("close seeded body: %v", closeErr)
		}
	}
	cache := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata,
		Objects:  contextBoundObjectStore{Store: objects},
	})
	service := &Service{cache: cache}
	entry, ok, err := service.cached(ctx, Route{Alias: key.Alias, Repository: key.Repository, Reference: key.Reference})
	if err != nil || !ok {
		t.Fatalf("cached = %#v, %t, %v; want hit", entry, ok, err)
	}
	body, err := io.ReadAll(entry.body)
	if err != nil {
		t.Fatalf("read cached body after lookup returned: %v", err)
	}
	if closeErr := entry.body.Close(); closeErr != nil {
		t.Fatalf("close cached body: %v", closeErr)
	}
	if string(body) != "cached artifact" {
		t.Fatalf("cached body = %q, want cached artifact", string(body))
	}
}

func TestStoredResponseKeepsReadContextUntilBodyClose(t *testing.T) {
	ctx := context.Background()
	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Errorf("close metadata: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open objects: %v", err)
	}
	service := &Service{cache: artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata,
		Objects:  contextBoundObjectStore{Store: objects},
	})}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	response, err := service.responseFromFetched(ctx, Request{Method: http.MethodGet}, Route{
		Alias: "central", Repository: "com/acme/demo/1.0", Reference: "demo-1.0.jar",
	}, &upstreamFetch{
		status:  http.StatusOK,
		headers: http.Header{"Content-Type": []string{"application/java-archive"}},
		body:    io.NopCloser(strings.NewReader("healthy upstream")),
	})
	if err != nil {
		t.Fatalf("responseFromFetched: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read stored response body after lookup returned: %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("close stored response body: %v", closeErr)
	}
	if string(body) != "healthy upstream" {
		t.Fatalf("stored response body = %q, want healthy upstream", string(body))
	}
}

type contextBoundObjectStore struct{ object.Store }

func (s contextBoundObjectStore) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	body, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return nil, nil, wrapError(err, "get context-bound object")
	}
	return contextBoundReadCloser{ctx: ctx, ReadCloser: body}, info, nil
}

type contextBoundReadCloser struct {
	ctx context.Context
	io.ReadCloser
}

func (r contextBoundReadCloser) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, wrapError(err, "read context-bound object")
	}
	n, err := r.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		return n, wrapError(err, "read context-bound object")
	}
	return n, err //nolint:wrapcheck // io.Reader must preserve the EOF sentinel.
}

func newCacheDegradationService(t *testing.T) (*Service, meta.Store) {
	t.Helper()
	metadata, err := meta.OpenSQLiteWithOptions(context.Background(), meta.DBOptions{
		Path: filepath.Join(t.TempDir(), "regimux.db"),
	})
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Errorf("close metadata: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Errorf("close metadata: %v", closeErr)
		}
		t.Fatalf("open objects: %v", err)
	}
	cache := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata,
		Objects:  objects,
	})
	service := &Service{cache: cache}
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	return service, metadata
}
