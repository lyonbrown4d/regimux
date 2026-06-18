package cache_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/events"
)

func TestBlobProxyStreamAndCacheFallsBackWhenSchedulerRejects(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	bus, fallbackEvents := captureStreamCacheFallbacks(t)
	proxy := cache.NewProxy(cache.ProxyDependencies{
		Client:              client,
		Metadata:            metadata,
		Objects:             objects,
		CacheConfig:         streamAndCacheConfig().Cache,
		BlobStreamScheduler: rejectingStreamScheduler{},
		Events:              bus,
	})

	result, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("blob get: %v", err)
	}
	assertFullBlobMiss(t, result, body)
	assertObjectPresence(ctx, t, objects, digest, false)
	assertBlobRequestCounters(t, client, 1, 0)
	event := receiveStreamCacheFallback(t, fallbackEvents)
	if event.Alias != "hub" || event.Reason != "scheduler_submit_failed" {
		t.Fatalf("unexpected stream cache fallback event: %#v", event)
	}
}

func TestBlobProxyStreamAndCacheUsesScheduler(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	scheduler := &recordingStreamScheduler{}
	proxy := cache.NewProxy(cache.ProxyDependencies{
		Client:              client,
		Metadata:            metadata,
		Objects:             objects,
		CacheConfig:         streamAndCacheConfig().Cache,
		BlobStreamScheduler: scheduler,
	})

	first, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	assertFullBlobMiss(t, first, body)
	waitObjectStored(ctx, t, objects, digest)

	second, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	assertFullBlobHit(t, second, body)
	assertBlobRequestCounters(t, client, 1, 0)
	if got := scheduler.calls(); got != 1 {
		t.Fatalf("scheduler calls = %d, want 1", got)
	}
}

func streamAndCacheConfig() config.Config {
	return config.Config{
		Cache: config.CacheConfig{
			Blob: config.BlobCacheConfig{
				StreamAndCache: true,
			},
		},
	}
}

type rejectingStreamScheduler struct{}

func (rejectingStreamScheduler) Submit(func()) error {
	return errors.New("stream scheduler saturated")
}

func captureStreamCacheFallbacks(t *testing.T) (events.Bus, <-chan events.ContainerPullStreamCacheFallback) {
	t.Helper()
	bus := events.NewBus(slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := bus.Close(); err != nil {
			t.Fatalf("close bus: %v", err)
		}
	})
	received := make(chan events.ContainerPullStreamCacheFallback, 1)
	unsubscribe, err := events.NewSubscriber(func(_ context.Context, event events.ContainerPullStreamCacheFallback) error {
		received <- event
		return nil
	}).Subscribe(bus)
	if err != nil {
		t.Fatalf("subscribe stream fallback: %v", err)
	}
	t.Cleanup(unsubscribe)
	return bus, received
}

func receiveStreamCacheFallback(t *testing.T, received <-chan events.ContainerPullStreamCacheFallback) events.ContainerPullStreamCacheFallback {
	t.Helper()
	select {
	case event := <-received:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream cache fallback event")
		return events.ContainerPullStreamCacheFallback{}
	}
}

type recordingStreamScheduler struct {
	mu        sync.Mutex
	submitted int
}

func (s *recordingStreamScheduler) Submit(task func()) error {
	s.mu.Lock()
	s.submitted++
	s.mu.Unlock()
	go task()
	return nil
}

func (s *recordingStreamScheduler) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.submitted
}
