package cache_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestManifestPrepareDefersColdPersistenceUntilCompletion(t *testing.T) {
	metadata, objects := newTestStores(t)
	tracked := &trackedManifestPersistenceStore{Store: metadata}
	client := readyManifestPrepareClient([]byte(`{"schemaVersion":2}`))
	proxy := newTestProxy(client, tracked, objects, backend.NewMemory(backend.MemoryOptions{}), config.Config{})
	service := requireManifestPreparer(t, proxy)

	manifest, complete, err := service.Prepare(t.Context(), cancellationManifestRequest())
	if err != nil || manifest == nil || complete == nil {
		t.Fatalf("prepare cold manifest: manifest=%t completion=%t error=%v", manifest != nil, complete != nil, err)
	}
	assertManifestPersistenceCounts(t, tracked, 0, 0, 0, 0)
	completeManifestResponse(t.Context(), t, complete)
	assertManifestPersistenceCounts(t, tracked, 1, 1, 1, 1)
}

func TestManifestPrepareSharedColdCompletionUsesLiveCoalescedCaller(t *testing.T) {
	ctx := t.Context()
	metadata, objects := newTestStores(t)
	tracked := &trackedManifestPersistenceStore{Store: metadata}
	client := &heldManifestCancellationClient{
		fakeRegistryClient: &fakeRegistryClient{manifestBody: []byte(`{"schemaVersion":2}`)},
		ownerEntered:       make(chan struct{}, 2), release: make(chan struct{}),
		body: []byte(`{"schemaVersion":2}`), mediaType: "application/vnd.oci.image.manifest.v1+json",
	}
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(client.release) }) })
	proxy := newTestProxy(client, tracked, objects, backend.NewMemory(backend.MemoryOptions{}), config.Config{})
	service := requireManifestPreparer(t, proxy)

	ownerCtx, cancelOwner := context.WithCancel(ctx)
	defer cancelOwner()
	owner := make(chan manifestPreparationResult, 1)
	go func() {
		manifest, complete, err := service.Prepare(ownerCtx, cancellationManifestRequest())
		owner <- manifestPreparationResult{manifest: manifest, complete: complete, err: err}
	}()
	waitForManifestCall(t, client.ownerEntered)
	follower := make(chan manifestPreparationResult, 1)
	go func() {
		manifest, complete, err := service.Prepare(ctx, cancellationManifestRequest())
		follower <- manifestPreparationResult{manifest: manifest, complete: complete, err: err}
	}()
	waitForManifestCancellationFlights(t, 2)
	cancelOwner()
	select {
	case got := <-owner:
		if got.err == nil || got.manifest != nil || got.complete != nil {
			t.Fatalf("canceled owner returned manifest=%t completion=%t error=%v", got.manifest != nil, got.complete != nil, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled owner waited for shared manifest load")
	}
	releaseOnce.Do(func() { close(client.release) })
	live := receiveManifestPreparation(t, follower)
	assertManifestPersistenceCounts(t, tracked, 0, 0, 0, 0)
	completeManifestResponse(ctx, t, live.complete)
	assertManifestPersistenceCounts(t, tracked, 1, 1, 1, 1)
}

func TestManifestPrepareCanonicalCompletionSurvivesCallerCancellationAfterUpsert(t *testing.T) {
	metadata, objects := newTestStores(t)
	tracked := &trackedManifestPersistenceStore{
		Store: metadata, upstreamEntered: make(chan struct{}, 1), upstreamRelease: make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(tracked.upstreamRelease) }) }
	t.Cleanup(release)
	client := readyManifestPrepareClient([]byte(`{"schemaVersion":2}`))
	proxy := newTestProxy(client, tracked, objects, backend.NewMemory(backend.MemoryOptions{}), config.Config{})
	service := requireManifestPreparer(t, proxy)
	_, complete, err := service.Prepare(t.Context(), cancellationManifestRequest())
	if err != nil || complete == nil {
		t.Fatalf("prepare manifest: completion=%t error=%v", complete != nil, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan error, 1)
	go func() { completed <- complete.Finalize(ctx) }()
	select {
	case <-tracked.upstreamEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("canonical upstream write did not commit")
	}
	cancel()
	release()
	if err := <-completed; err != nil {
		t.Fatalf("completion after caller cancellation: %v", err)
	}
	completeManifestResponse(t.Context(), t, complete)
	if got := tracked.upstreamPulls.Load(); got != 1 {
		t.Errorf("canonical upstream writes=%d, want exactly one", got)
	}
}

func TestManifestPrepareFinalizesCanonicalMetadataWhenCallerCanceled(t *testing.T) {
	metadata, objects := newTestStores(t)
	tracked := &trackedManifestPersistenceStore{Store: metadata}
	proxy := newTestProxy(readyManifestPrepareClient([]byte(`{"schemaVersion":2}`)), tracked, objects, backend.NewMemory(backend.MemoryOptions{}), config.Config{})
	service := requireManifestPreparer(t, proxy)
	_, complete, err := service.Prepare(t.Context(), cancellationManifestRequest())
	if err != nil || complete == nil {
		t.Fatalf("prepare manifest: completion=%t error=%v", complete != nil, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := complete.CompleteResponse(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled completion error=%v, want context canceled", err)
	}
	assertManifestPersistenceCounts(t, tracked, 1, 1, 1, 0)
}

func TestManifestPrepareObjectPutFailureStillRecordsSuccessfulUpstreamFetch(t *testing.T) {
	metadata, objects := newTestStores(t)
	tracked := &trackedManifestPersistenceStore{Store: metadata}
	client := readyManifestPrepareClient([]byte(`{"schemaVersion":2}`))
	proxy := newTestProxy(client, tracked, failingManifestObjectStore{Store: objects}, backend.NewMemory(backend.MemoryOptions{}), config.Config{})
	service := requireManifestPreparer(t, proxy)
	_, complete, err := service.Prepare(t.Context(), cancellationManifestRequest())
	if err != nil || complete == nil {
		t.Fatalf("prepare manifest after object failure: completion=%t error=%v", complete != nil, err)
	}
	completeManifestResponse(t.Context(), t, complete)
	assertManifestPersistenceCounts(t, tracked, 1, 0, 0, 1)
}

func TestManifestPrepareFailedRevalidationDoesNotCreateFreshCacheEntry(t *testing.T) {
	metadata, objects, backendCache, client, cfg := seedExpiredManifest(t)
	client.manifestHeads = 0
	proxy := newTestProxy(client, failingManifestTagStore{Store: metadata}, &alternatingManifestObjectStore{Store: objects}, backendCache, cfg)
	service := requireManifestPreparer(t, proxy)
	_, complete, prepareErr := service.Prepare(t.Context(), cancellationManifestRequest())
	if prepareErr != nil || complete == nil {
		t.Fatalf("prepare revalidated manifest: completion=%t error=%v", complete != nil, prepareErr)
	}
	if completionErr := complete.CompleteResponse(t.Context()); completionErr == nil {
		t.Fatal("revalidation completion hid tag persistence failure")
	}
	key := "manifest:hub:library/alpine:latest:" + reference.AcceptKey("")
	if _, found, cacheErr := backendCache.Get(t.Context(), key); cacheErr != nil || found {
		t.Fatalf("failed revalidation left a cache envelope: found=%t error=%v", found, cacheErr)
	}
	_, _, secondErr := service.Prepare(t.Context(), cancellationManifestRequest())
	if secondErr != nil {
		t.Fatalf("second prepare after failed revalidation: %v", secondErr)
	}
	if client.manifestHeads != 2 {
		t.Errorf("revalidation HEAD calls=%d, want another HEAD after failed save", client.manifestHeads)
	}
}

func TestManifestPrepareSuccessfulRevalidationRebuildsCacheFromMetadata(t *testing.T) {
	metadata, objects, backendCache, client, cfg := seedExpiredManifest(t)
	client.manifestHeads = 0
	proxy := newTestProxy(client, metadata, &firstObjectReadMissStore{Store: objects}, backendCache, cfg)
	service := requireManifestPreparer(t, proxy)
	assertSuccessfulRevalidationCacheRebuilt(t, service, backendCache, client)
}

func assertSuccessfulRevalidationCacheRebuilt(t *testing.T, service cache.ManifestPreparer, backendCache backend.Backend, client *fakeRegistryClient) {
	t.Helper()
	_, complete, prepareErr := service.Prepare(t.Context(), cancellationManifestRequest())
	if prepareErr != nil || complete == nil {
		t.Fatalf("prepare revalidated manifest: completion=%t error=%v", complete != nil, prepareErr)
	}
	if err := complete.CompleteResponse(t.Context()); err != nil {
		t.Fatalf("complete revalidation metadata: %v", err)
	}
	manifest, nextComplete, readErr := service.Prepare(t.Context(), cancellationManifestRequest())
	if readErr != nil || manifest == nil || nextComplete == nil {
		t.Fatalf("read persisted revalidation: manifest=%t completion=%t error=%v", manifest != nil, nextComplete != nil, readErr)
	}
	if client.manifestHeads != 1 {
		t.Errorf("successful revalidation triggered %d HEAD calls, want one", client.manifestHeads)
	}
	assertRevalidationCacheHit(t, manifest, backendCache)
}

func assertRevalidationCacheHit(t *testing.T, manifest *cache.CachedManifest, backendCache backend.Backend) {
	t.Helper()
	if manifest.Cache != cache.CacheHit {
		t.Errorf("metadata lookup cache status=%s, want cache hit", manifest.Cache)
	}
	if _, found, err := backendCache.Get(t.Context(), "manifest:hub:library/alpine:latest:"+reference.AcceptKey("")); err != nil || !found {
		t.Errorf("next metadata lookup did not rebuild cache envelope: found=%t error=%v", found, err)
	}
}

func seedExpiredManifest(t *testing.T) (meta.Store, object.Store, backend.Backend, *fakeRegistryClient, config.Config) {
	t.Helper()
	metadata, objects := newTestStores(t)
	backendCache := backend.NewMemory(backend.MemoryOptions{})
	client := readyManifestPrepareClient([]byte(`{"schemaVersion":2}`))
	cfg := config.Config{Cache: config.CacheConfig{Manifest: config.ManifestCacheConfig{TagTTL: time.Hour}}}
	proxy := newTestProxy(client, metadata, objects, backendCache, cfg)
	if _, err := proxy.Manifests().Get(t.Context(), cancellationManifestRequest()); err != nil {
		t.Fatalf("seed manifest metadata: %v", err)
	}
	tag, found, err := metadata.Tag(t.Context(), meta.TagKey{Alias: "hub", Repository: "library/alpine", Reference: "latest"})
	if err != nil || !found {
		t.Fatalf("read seeded manifest tag: found=%t error=%v", found, err)
	}
	tag.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := metadata.UpsertTag(t.Context(), *tag); err != nil {
		t.Fatalf("expire manifest tag: %v", err)
	}
	key := "manifest:hub:library/alpine:latest:" + reference.AcceptKey("")
	if err := backendCache.Delete(t.Context(), key); err != nil {
		t.Fatalf("expire manifest envelope: %v", err)
	}
	return metadata, objects, backendCache, client, cfg
}

func readyManifestPrepareClient(body []byte) *fakeRegistryClient {
	return &fakeRegistryClient{manifestBody: body, manifestMedia: "application/vnd.oci.image.manifest.v1+json"}
}

func assertManifestPersistenceCounts(t *testing.T, store *trackedManifestPersistenceStore, upstreamPulls, manifests, tags, pulls int64) {
	t.Helper()
	if got := store.upstreamPulls.Load(); got != upstreamPulls {
		t.Errorf("upstream pull writes=%d, want %d", got, upstreamPulls)
	}
	if got := store.manifests.Load(); got != manifests {
		t.Errorf("manifest writes=%d, want %d", got, manifests)
	}
	if got := store.tags.Load(); got != tags {
		t.Errorf("tag writes=%d, want %d", got, tags)
	}
	if got := store.pulls.Load(); got != pulls {
		t.Errorf("HTTP pull writes=%d, want %d", got, pulls)
	}
}
