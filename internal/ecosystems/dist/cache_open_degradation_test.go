package dist_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestServiceFallsBackWhenWarmDistObjectOpenExceedsTransferTimeout(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if upstreamRequests.Add(1) > 1 {
			time.Sleep(1200 * time.Millisecond)
		}
		w.Header().Set(distribution.HeaderContentType, "application/zip")
		writeBody(t, w, testZIPBody)
	}))
	t.Cleanup(upstream.Close)

	seed, metadata, objects := newTestService(context.Background(), t, upstream.URL, nil)
	warm, err := seed.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "warm cache", err)
	closeBody(t, warm.Body)
	upstreamRequests.Store(0)

	delayed := &delayedDistObjectStore{Store: objects, delay: 2200 * time.Millisecond}
	service := newDistServiceWithObjects(upstream.URL, metadata, delayed)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	resp, getErr := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	elapsed := time.Since(started)
	requireNoError(t, "get after slow cache open", getErr)
	if elapsed >= 5*time.Second {
		t.Fatalf("Get took %s, want one cache timeout and healthy upstream fill within caller's 5s budget", elapsed)
	}
	if got := readResponse(t, resp); got != testZIPBody {
		t.Fatalf("body=%q want cached artifact bytes", got)
	}
	if got := delayed.gets.Load(); got != 1 {
		t.Fatalf("cache object opens=%d want exactly 1 for this request", got)
	}
	if got := upstreamRequests.Load(); got != 1 {
		t.Fatalf("upstream requests=%d want one fallback", got)
	}
}

func TestServiceCallerCancellationDuringDistObjectOpenDoesNotFetchUpstream(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		writeBody(t, w, testZIPBody)
	}))
	t.Cleanup(upstream.Close)

	seed, metadata, objects := newTestService(context.Background(), t, upstream.URL, nil)
	warm, err := seed.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "warm cache", err)
	closeBody(t, warm.Body)
	upstreamRequests.Store(0)

	blocked := &blockedDistObjectStore{Store: objects, started: make(chan struct{})}
	service := newDistServiceWithObjects(upstream.URL, metadata, blocked)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, getErr := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
		done <- getErr
	}()
	select {
	case <-blocked.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cached object open")
	}
	cancel()
	var getErr error
	select {
	case getErr = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for canceled Get")
	}
	if !errors.Is(getErr, context.Canceled) {
		t.Fatalf("Get error=%v want caller cancellation", getErr)
	}
	if got := upstreamRequests.Load(); got != 0 {
		t.Fatalf("upstream requests=%d want 0 after caller cancellation", got)
	}
}

func TestServiceCallerDeadlineDuringDistObjectOpenDoesNotFetchUpstream(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		writeBody(t, w, testZIPBody)
	}))
	t.Cleanup(upstream.Close)

	seed, metadata, objects := newTestService(context.Background(), t, upstream.URL, nil)
	warm, err := seed.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "warm cache", err)
	closeBody(t, warm.Body)
	upstreamRequests.Store(0)

	blocked := &blockedDistObjectStore{Store: objects, started: make(chan struct{})}
	service := newDistServiceWithObjects(upstream.URL, metadata, blocked)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Get error=%v want caller deadline", err)
	}
	if got := upstreamRequests.Load(); got != 0 {
		t.Fatalf("upstream requests=%d want 0 after caller deadline", got)
	}
}

func TestServiceRetriesUpstreamAfterTransientDistObjectOpenError(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		w.Header().Set(distribution.HeaderContentType, "application/zip")
		writeBody(t, w, testZIPBody)
	}))
	t.Cleanup(upstream.Close)

	seed, metadata, objects := newTestService(context.Background(), t, upstream.URL, nil)
	warm, err := seed.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "warm cache", err)
	closeBody(t, warm.Body)
	upstreamRequests.Store(0)

	transient := &transientDistObjectStore{Store: objects}
	service := newDistServiceWithObjects(upstream.URL, metadata, transient)
	for range 2 {
		resp, getErr := service.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
		requireNoError(t, "get with transient object store failure", getErr)
		if body := readResponse(t, resp); body != testZIPBody {
			t.Fatalf("body=%q want artifact bytes", body)
		}
	}
	if got := transient.gets.Load(); got != 2 {
		t.Fatalf("cache object opens=%d want 2", got)
	}
	if got := upstreamRequests.Load(); got != 1 {
		t.Fatalf("upstream requests=%d want one fallback after transient error", got)
	}
}

func TestServiceDoesNotServeStaleDistBodyWhenCacheOpenAndUpstreamFail(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if upstreamRequests.Add(1) == 1 {
			w.Header().Set(distribution.HeaderContentType, "application/zip")
			writeBody(t, w, testZIPBody)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		writeBody(t, w, "origin unavailable")
	}))
	t.Cleanup(upstream.Close)
	seed, metadata, objects := newTestService(context.Background(), t, upstream.URL, nil)
	warm, err := seed.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "warm cache", err)
	closeBody(t, warm.Body)

	service := newDistServiceWithObjects(upstream.URL, metadata, &transientDistObjectStore{Store: objects, failEvery: true})
	resp, err := service.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "get after cache and upstream failures", err)
	if resp.Status != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503", resp.Status)
	}
	if body := readResponse(t, resp); body != "origin unavailable" {
		t.Fatalf("body=%q want upstream error body", body)
	}
}
