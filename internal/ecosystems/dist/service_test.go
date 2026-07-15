//revive:disable:file-length-limit Dist service scenarios stay grouped to share fixtures and protocol assertions.
package dist_test

import (
	"context"
	"github.com/lyonbrown4d/regimux/internal/testkit"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestServiceCachesFullDistArtifact(t *testing.T) {
	ctx := context.Background()
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/gradle-8.7-bin.zip" {
			t.Fatalf("upstream path = %q", r.URL.Path)
		}
		w.Header().Set(distribution.HeaderContentType, "application/zip")
		w.Header().Set(distribution.HeaderETag, `"gradle"`)
		writeBody(t, w, "abcdef")
	}))
	t.Cleanup(upstream.Close)
	service, metadata, objects := newTestService(ctx, t, upstream.URL, []string{"gradle-*-bin.zip"})

	first, err := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "first get", err)
	if first.Cache != artifactcache.CacheMiss {
		t.Fatalf("first cache = %q", first.Cache)
	}
	if body := readResponse(t, first); body != "abcdef" {
		t.Fatalf("first body = %q", body)
	}
	assertStoredArtifact(ctx, t, metadata, objects, meta.TagKey{
		Alias:      "gradle",
		Repository: "dist",
		Reference:  "gradle-8.7-bin.zip",
	}, "abcdef", "application/zip")

	second, err := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "second get", err)
	if second.Cache != artifactcache.CacheHit {
		t.Fatalf("second cache = %q", second.Cache)
	}
	if body := readResponse(t, second); body != "abcdef" {
		t.Fatalf("second body = %q", body)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

func TestServiceCoalescesConcurrentFullArtifactMiss(t *testing.T) {
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
		if r.URL.Path != "/gradle-8.7-bin.zip" {
			t.Fatalf("upstream path = %q", r.URL.Path)
		}
		<-release
		w.Header().Set(distribution.HeaderContentType, "application/zip")
		writeBody(t, w, "abcdef")
	}))
	t.Cleanup(upstream.Close)
	service, _, _ := newTestService(ctx, t, upstream.URL, []string{"gradle-*-bin.zip"})

	const clients = 8
	run := testkit.StartConcurrent(clients, func() (*dist.Response, error) {
		return service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	})
	testkit.WaitForSignal(t, started)
	releaseOnce.Do(func() { close(release) })

	responses := run.Wait(t)
	testkit.RequireOneMiss(t, responses, artifactcache.CacheMiss, artifactcache.CacheHit, func(resp *dist.Response) string {
		if body := readResponse(t, resp); body != "abcdef" {
			t.Fatalf("body = %q", body)
		}
		return resp.Cache
	})
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}
func TestServiceHeadDoesNotCacheDistArtifact(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("upstream method = %s", r.Method)
		}
		w.Header().Set(distribution.HeaderContentLength, "6")
		w.Header().Set(distribution.HeaderContentType, "application/zip")
	}))
	t.Cleanup(upstream.Close)
	service, metadata, _ := newTestService(ctx, t, upstream.URL, []string{"gradle-*.zip"})

	resp, err := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodHead})
	requireNoError(t, "head", err)
	if resp.Cache != artifactcache.CacheMiss {
		t.Fatalf("head cache = %q", resp.Cache)
	}
	_, ok, err := metadata.Tag(ctx, meta.TagKey{Alias: "gradle", Repository: "dist", Reference: "gradle-8.7-bin.zip"})
	requireNoError(t, "lookup tag", err)
	if ok {
		t.Fatal("HEAD request stored dist metadata")
	}
}

func TestServiceRangeMissPassesThroughWithoutCaching(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=2-3" {
			t.Fatalf("upstream range = %q", got)
		}
		w.Header().Set(distribution.HeaderContentRange, "bytes 2-3/6")
		w.Header().Set(distribution.HeaderContentLength, "2")
		w.WriteHeader(http.StatusPartialContent)
		writeBody(t, w, "cd")
	}))
	t.Cleanup(upstream.Close)
	service, metadata, _ := newTestService(ctx, t, upstream.URL, []string{"gradle-*.zip"})

	resp, err := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet, Range: "bytes=2-3"})
	requireNoError(t, "range get", err)
	if resp.Status != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.Status)
	}
	if body := readResponse(t, resp); body != "cd" {
		t.Fatalf("range body = %q", body)
	}
	_, ok, err := metadata.Tag(ctx, meta.TagKey{Alias: "gradle", Repository: "dist", Reference: "gradle-8.7-bin.zip"})
	requireNoError(t, "lookup tag", err)
	if ok {
		t.Fatal("range miss stored partial dist metadata")
	}
}

func TestServiceRangeHitReadsCachedObject(t *testing.T) {
	ctx := context.Background()
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set(distribution.HeaderContentType, "application/zip")
		w.Header().Set(distribution.HeaderContentLength, "6")
		writeBody(t, w, "abcdef")
	}))
	t.Cleanup(upstream.Close)
	service, _, _ := newTestService(ctx, t, upstream.URL, []string{"gradle-*.zip"})

	full, err := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "full get", err)
	closeBody(t, full.Body)

	ranged, err := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet, Range: "bytes=2-3"})
	requireNoError(t, "range hit", err)
	if ranged.Status != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", ranged.Status)
	}
	if got := ranged.Headers.Get(distribution.HeaderContentRange); got != "bytes 2-3/6" {
		t.Fatalf("content-range = %q", got)
	}
	if got := ranged.Headers.Get(distribution.HeaderContentLength); got != "2" {
		t.Fatalf("content-length = %q", got)
	}
	if body := readResponse(t, ranged); body != "cd" {
		t.Fatalf("range body = %q", body)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

func TestServiceFallsBackToMirrorOnDistHTTPStatus(t *testing.T) {
	ctx := context.Background()
	var primaryRequests atomic.Int64
	var mirrorRequests atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryRequests.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(primary.Close)
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorRequests.Add(1)
		if r.URL.Path != "/gradle-8.7-bin.zip" {
			t.Fatalf("mirror path = %q", r.URL.Path)
		}
		w.Header().Set(distribution.HeaderContentType, "application/zip")
		writeBody(t, w, "mirror-body")
	}))
	t.Cleanup(mirror.Close)
	service, metadata, objects := newTestServiceWithMirrors(ctx, t, primary.URL, []string{mirror.URL}, []string{"gradle-*.zip"})

	resp, err := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "get with mirror fallback", err)
	if resp.Cache != artifactcache.CacheMiss {
		t.Fatalf("cache = %q", resp.Cache)
	}
	if body := readResponse(t, resp); body != "mirror-body" {
		t.Fatalf("body = %q", body)
	}
	if got := primaryRequests.Load(); got != 1 {
		t.Fatalf("primary requests = %d, want 1", got)
	}
	if got := mirrorRequests.Load(); got != 1 {
		t.Fatalf("mirror requests = %d, want 1", got)
	}
	assertStoredArtifact(ctx, t, metadata, objects, meta.TagKey{
		Alias:      "gradle",
		Repository: "dist",
		Reference:  "gradle-8.7-bin.zip",
	}, "mirror-body", "application/zip")
}

func TestServiceDoesNotFallbackToMirrorOnDistForbidden(t *testing.T) {
	ctx := context.Background()
	var mirrorRequests atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(primary.Close)
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorRequests.Add(1)
		writeBody(t, w, "mirror-body")
	}))
	t.Cleanup(mirror.Close)
	service, _, _ := newTestServiceWithMirrors(ctx, t, primary.URL, []string{mirror.URL}, []string{"gradle-*.zip"})

	resp, err := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "get forbidden", err)
	if resp.Status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.Status)
	}
	if body := readResponse(t, resp); !strings.Contains(body, "forbidden") {
		t.Fatalf("body = %q", body)
	}
	if got := mirrorRequests.Load(); got != 0 {
		t.Fatalf("mirror requests = %d, want 0", got)
	}
}

func TestServiceRejectsDisallowedPath(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(upstream.Close)
	service, metadata, _ := newTestService(ctx, t, upstream.URL, []string{"gradle-*-bin.zip"})

	_, err := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "evil.exe", Method: http.MethodGet})
	if err == nil {
		t.Fatal("expected disallowed path error")
	}
	pull, ok, err := metadata.Pull(ctx, meta.PullKey{Alias: "gradle", Repository: "dist", Reference: "evil.exe"})
	requireNoError(t, "lookup denied pull", err)
	if !ok || pull.PolicyDeniedCount != 1 {
		t.Fatalf("policy denied pull = %#v ok=%v", pull, ok)
	}
}
