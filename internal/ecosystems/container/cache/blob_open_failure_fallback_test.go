package cache_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestWarmBlobOpenTimeoutFallsBackWithRepositoryAuthAndDigest(t *testing.T) {
	body := []byte("verified fallback blob")
	digest := testDigestFor(body)
	metadata, objects := newTestStores(t)
	seedStoredBlob(t, metadata, objects, digest, body)
	delayed := &delayedFirstBlobOpen{Store: objects, delay: 2200 * time.Millisecond}
	var upstreamCalls atomic.Int32
	registry := authenticatedBlobRegistry(t, digest, body, &upstreamCalls)
	t.Cleanup(registry.Close)
	client := newCacheTestUpstreamClient(map[string]upstream.Config{
		"hub": {Registry: registry.URL, Auth: upstream.AuthConfig{Type: "bearer", Token: "fallback-token"}},
	})
	proxy := newTestProxy(client, metadata, delayed, nil, streamAndCacheConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	result, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet,
	})
	if err != nil {
		t.Fatalf("warm blob get after object-open timeout: %v", err)
	}
	assertWarmOpenFallbackResult(t, result, body, time.Since(started), delayed, &upstreamCalls)
}

func authenticatedBlobRegistry(t *testing.T, digest string, body []byte, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assertBlobRegistryRequest(t, r, digest)
		w.Header().Set(distribution.HeaderDockerContentDigest, digest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		if _, err := w.Write(body); err != nil {
			t.Errorf("write registry body: %v", err)
		}
	}))
}

func assertBlobRegistryRequest(t *testing.T, request *http.Request, digest string) {
	t.Helper()
	if request.URL.Path != "/v2/library/alpine/blobs/"+digest {
		t.Errorf("registry path=%q", request.URL.Path)
	}
	if got := request.Header.Get(distribution.HeaderAuthorization); got != "Bearer fallback-token" {
		t.Errorf("Authorization=%q", got)
	}
}

func assertWarmOpenFallbackResult(
	t *testing.T,
	result *cache.BlobReadResult,
	body []byte,
	elapsed time.Duration,
	delayed *delayedFirstBlobOpen,
	upstreamCalls *atomic.Int32,
) {
	t.Helper()
	if got := readAndClose(t, result.Reader); !bytes.Equal(got, body) {
		t.Fatalf("fallback body=%q", got)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("fallback took %s, want completion before caller deadline", elapsed)
	}
	if got := delayed.gets.Load(); got != 1 {
		t.Fatalf("object Get attempts=%d, want one failed warm open", got)
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls=%d, want one authenticated fallback", got)
	}
}

func TestColdBlobPostStoreOpenFailureFallsBackWithoutReopening(t *testing.T) {
	body := []byte("cold reopen fallback")
	digest := testDigestFor(body)
	metadata, objects := newTestStores(t)
	failing := &failFirstBlobOpen{Store: objects}
	var upstreamCalls atomic.Int32
	registry := blobRegistryServer(t, digest, body, &upstreamCalls)
	client := newCacheTestUpstreamClient(map[string]upstream.Config{"hub": {Registry: registry.URL}})
	proxy := newTestProxy(client, metadata, failing, nil, config.Config{})
	result, err := proxy.Blobs().Get(context.Background(), cache.BlobRequest{
		UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet,
	})
	if err != nil {
		t.Fatalf("cold blob get after stored-object reopen failure: %v", err)
	}
	if got := readAndClose(t, result.Reader); !bytes.Equal(got, body) {
		t.Fatalf("fallback body=%q", got)
	}
	if got := failing.gets.Load(); got != 1 {
		t.Fatalf("object Get attempts=%d, want one failed post-store open", got)
	}
	if got := upstreamCalls.Load(); got != 2 {
		t.Fatalf("upstream calls=%d, want initial store plus one fallback", got)
	}
}

func TestBlobStatFailureDoesNotUseOpenFallback(t *testing.T) {
	body := []byte("stat failure is not an open failure")
	digest := testDigestFor(body)
	metadata, objects := newTestStores(t)
	seedStoredBlob(t, metadata, objects, digest, body)
	statErr := errors.New("injected object stat failure")
	broken := &failBlobStat{Store: objects, err: statErr}
	var upstreamCalls atomic.Int32
	registry := blobRegistryServer(t, digest, body, &upstreamCalls)
	client := newCacheTestUpstreamClient(map[string]upstream.Config{"hub": {Registry: registry.URL}})
	proxy := newTestProxy(client, metadata, broken, nil, config.Config{})
	_, err := proxy.Blobs().Get(context.Background(), cache.BlobRequest{
		UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet,
	})
	if !errors.Is(err, statErr) {
		t.Fatalf("Get error=%v, want stat error", err)
	}
	if got := upstreamCalls.Load(); got != 0 {
		t.Fatalf("upstream calls after stat failure=%d, want zero", got)
	}
}

func TestWarmBlobRangeOpenFailureFallsBackWithOriginalRange(t *testing.T) {
	body := []byte("0123456789abcdef")
	digest := testDigestFor(body)
	metadata, objects := newTestStores(t)
	seedStoredBlob(t, metadata, objects, digest, body)
	failing := &failFirstBlobOpen{Store: objects}
	var upstreamCalls atomic.Int32
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if got := r.Header.Get("Range"); got != "bytes=2-5" {
			t.Errorf("Range=%q", got)
		}
		w.Header().Set(distribution.HeaderDockerContentDigest, digest)
		w.Header().Set(distribution.HeaderContentRange, "bytes 2-5/16")
		w.Header().Set(distribution.HeaderContentLength, "4")
		w.WriteHeader(http.StatusPartialContent)
		if _, err := w.Write(body[2:6]); err != nil {
			t.Errorf("write range body: %v", err)
		}
	}))
	t.Cleanup(registry.Close)
	client := newCacheTestUpstreamClient(map[string]upstream.Config{"hub": {Registry: registry.URL}})
	proxy := newTestProxy(client, metadata, failing, nil, config.Config{})
	result, err := proxy.Blobs().Get(context.Background(), cache.BlobRequest{
		UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest,
		Range: &object.HTTPRange{Start: 2, End: 5}, Method: http.MethodGet,
	})
	if err != nil {
		t.Fatalf("range blob get after object-open failure: %v", err)
	}
	if got := readAndClose(t, result.Reader); !bytes.Equal(got, body[2:6]) {
		t.Fatalf("range fallback body=%q", got)
	}
	if result.Status != http.StatusPartialContent || result.Headers.Get(distribution.HeaderContentRange) != "bytes 2-5/16" {
		t.Fatalf("range fallback status/content-range=%d/%q", result.Status, result.Headers.Get(distribution.HeaderContentRange))
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls=%d, want one range fallback", got)
	}
}

func TestWarmBlobOpenFallbackRejectsWrongUpstreamBodyDigest(t *testing.T) {
	body := []byte("correct blob body")
	digest := testDigestFor(body)
	metadata, objects := newTestStores(t)
	seedStoredBlob(t, metadata, objects, digest, body)
	failing := &failFirstBlobOpen{Store: objects}
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(distribution.HeaderDockerContentDigest, digest)
		wrongBody := []byte("wrong blob body!!")
		w.Header().Set("Content-Length", strconv.Itoa(len(wrongBody)))
		if _, err := w.Write(wrongBody); err != nil {
			t.Errorf("write wrong digest body: %v", err)
		}
	}))
	t.Cleanup(registry.Close)
	client := newCacheTestUpstreamClient(map[string]upstream.Config{"hub": {Registry: registry.URL}})
	proxy := newTestProxy(client, metadata, failing, nil, streamAndCacheConfig())
	result, err := proxy.Blobs().Get(context.Background(), cache.BlobRequest{
		UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet,
	})
	if err != nil {
		t.Fatalf("get after cache open failure: %v", err)
	}
	_, readErr := io.ReadAll(result.Reader)
	closeErr := result.Reader.Close()
	if readErr == nil && closeErr == nil {
		t.Fatal("wrong upstream blob bytes were returned without a digest error")
	}
}

func TestWarmBlobOpenFailureDoesNotFallbackAfterCallerCancellation(t *testing.T) {
	body := []byte("cancel fallback")
	digest := testDigestFor(body)
	metadata, objects := newTestStores(t)
	seedStoredBlob(t, metadata, objects, digest, body)
	blocked := &blockedFirstBlobOpen{Store: objects, started: make(chan struct{})}
	var upstreamCalls atomic.Int32
	registry := blobRegistryServer(t, digest, body, &upstreamCalls)
	client := newCacheTestUpstreamClient(map[string]upstream.Config{"hub": {Registry: registry.URL}})
	proxy := newTestProxy(client, metadata, blocked, nil, config.Config{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, getErr := proxy.Blobs().Get(ctx, cache.BlobRequest{
			UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet,
		})
		done <- getErr
	}()
	select {
	case <-blocked.started:
	case <-time.After(5 * time.Second):
		t.Fatal("cache object open did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Get error=%v, want caller cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled Get did not stop")
	}
	if got := upstreamCalls.Load(); got != 0 {
		t.Fatalf("upstream calls=%d after caller cancellation", got)
	}
}

func TestWarmBlobHeadDoesNotOpenObjectBody(t *testing.T) {
	body := []byte("head without object body")
	digest := testDigestFor(body)
	metadata, objects := newTestStores(t)
	seedStoredBlob(t, metadata, objects, digest, body)
	failing := &failFirstBlobOpen{Store: objects}
	registry := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("warm HEAD unexpectedly fetched upstream")
	}))
	t.Cleanup(registry.Close)
	client := newCacheTestUpstreamClient(map[string]upstream.Config{"hub": {Registry: registry.URL}})
	proxy := newTestProxy(client, metadata, failing, nil, config.Config{})
	result, err := proxy.Blobs().Get(context.Background(), cache.BlobRequest{
		UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodHead,
	})
	if err != nil {
		t.Fatalf("warm HEAD: %v", err)
	}
	if got := readAndClose(t, result.Reader); len(got) != 0 {
		t.Fatalf("HEAD body=%q", got)
	}
	if got := failing.gets.Load(); got != 0 {
		t.Fatalf("object Get attempts=%d, want no body open for HEAD", got)
	}
}
