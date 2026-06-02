package goproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
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
		_, _ = w.Write([]byte("module github.com/acme/lib\n"))
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL)
	first, err := service.Get(ctx, Request{
		Alias: "golang",
		Tail:  "github.com/acme/lib/@v/v1.2.3.mod",
	})
	requireNoError(t, "first get", err)
	assertBody(t, first, "module github.com/acme/lib\n")
	if first.Cache != cacheMiss {
		t.Fatalf("first cache = %q, want %q", first.Cache, cacheMiss)
	}

	second, err := service.Get(ctx, Request{
		Alias: "golang",
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

func TestServicePassesThroughNotFound(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL)
	resp, err := service.Get(ctx, Request{
		Alias: "golang",
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
	for i := 0; i < 2; i++ {
		resp, err := service.Get(ctx, Request{
			Alias:  "golang",
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
	service := NewService(ServiceDependencies{
		Config: config.Config{
			Upstreams: map[string]config.UpstreamConfig{
				"hub": {Type: "oci", Registry: "https://registry-1.docker.io"},
			},
		},
	})
	_, err := service.Get(ctx, Request{
		Alias: "hub",
		Tail:  "github.com/acme/lib/@v/v1.2.3.mod",
	})
	if err == nil {
		t.Fatal("expected non-go upstream error")
	}
}

func newTestService(ctx context.Context, t *testing.T, upstreamURL string) *Service {
	t.Helper()
	db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", db.Close())
	})
	objects, err := object.NewMemory("go-proxy-test")
	requireNoError(t, "open objects", err)
	return NewService(ServiceDependencies{
		Config: config.Config{
			Upstreams: map[string]config.UpstreamConfig{
				"golang": {
					Type:     "go",
					Registry: upstreamURL,
				},
			},
		},
		Metadata: db,
		Objects:  objects,
	})
}

func assertBody(t *testing.T, resp *Response, want string) {
	t.Helper()
	if resp == nil || resp.Body == nil {
		t.Fatalf("response body is empty")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	requireNoError(t, "read body", err)
	if string(body) != want {
		t.Fatalf("body = %q, want %q", string(body), want)
	}
}

func requireNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}
