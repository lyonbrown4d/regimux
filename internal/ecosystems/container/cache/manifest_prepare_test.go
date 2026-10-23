package cache_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestManifestPrepareDefersMetadataAndCompletesOnceWithCallerContext(t *testing.T) {
	service, metadata, backendCache, body := newManifestPreparationFixture(t)
	prepareCtx, cancelPrepare := context.WithCancel(t.Context())
	defer cancelPrepare()
	manifest, complete, err := service.Prepare(prepareCtx, cancellationManifestRequest())
	if err != nil || complete == nil {
		t.Fatalf("prepare manifest: completion=%t error=%v", complete != nil, err)
	}
	if metadata.pulls.Load() != 0 || metadata.touches.Load() != 0 {
		t.Fatal("Prepare recorded a pull or touched metadata before response preparation finished")
	}
	flightCtx := <-backendCache.entered
	select {
	case <-flightCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("completed manifest load retained its singleflight context")
	}
	cancelPrepare()
	completionCtx := context.WithValue(t.Context(), manifestCompletionContextKey{}, "completion")
	var callers sync.WaitGroup
	for range 32 {
		callers.Go(func() { completeManifestResponse(completionCtx, t, complete) })
	}
	callers.Wait()
	assertPreparedManifestPulls(t, metadata, 1)
	if metadata.touches.Load() != 1 {
		t.Errorf("metadata touches=%d, want 1", metadata.touches.Load())
	}
	if !bytes.Equal(manifest.Body, body) {
		t.Errorf("completion changed prepared body: %q", manifest.Body)
	}
}

func TestManifestPrepareSharedLoadKeepsIndependentCompletions(t *testing.T) {
	service, metadata, backendCache, body := newManifestPreparationFixture(t)
	gate := make(chan struct{})
	backendCache.gate = gate
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate) }) }
	t.Cleanup(unblock)
	results := make(chan manifestPreparationResult, 2)
	start := func() {
		go func() {
			manifest, complete, err := service.Prepare(t.Context(), cancellationManifestRequest())
			results <- manifestPreparationResult{manifest: manifest, complete: complete, err: err}
		}()
	}
	start()
	select {
	case <-backendCache.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("manifest load did not reach cache")
	}
	start()
	// The two callers and their shared loader must be in the same flight
	// before releasing the cache, so this exercises an actually shared result.
	waitForManifestCancellationFlights(t, 3)
	unblock()
	first := receiveManifestPreparation(t, results)
	second := receiveManifestPreparation(t, results)
	if backendCache.reads.Load() != 1 {
		t.Fatalf("shared manifest cache reads=%d, want 1", backendCache.reads.Load())
	}
	if metadata.pulls.Load() != 0 || metadata.touches.Load() != 0 {
		t.Fatal("shared Prepare performed metadata writes before completion")
	}
	completionCtx := context.WithValue(t.Context(), manifestCompletionContextKey{}, "completion")
	var callers sync.WaitGroup
	for _, result := range []manifestPreparationResult{first, second} {
		callers.Go(func() {
			completeManifestResponse(completionCtx, t, result.complete)
			completeManifestResponse(completionCtx, t, result.complete)
		})
	}
	callers.Wait()
	assertPreparedManifestPulls(t, metadata, 2)
	if !bytes.Equal(first.manifest.Body, body) || !bytes.Equal(second.manifest.Body, body) {
		t.Fatal("independent completions changed a shared manifest body")
	}
}

func newManifestPreparationFixture(t *testing.T) (cache.ManifestPreparer, *manifestPreparationMetadata, *manifestPreparationBackend, []byte) {
	t.Helper()
	store, objects := newTestStores(t)
	metadata := &manifestPreparationMetadata{Store: store}
	backendCache := &manifestPreparationBackend{
		Backend: backend.NewMemory(backend.MemoryOptions{}), entered: make(chan context.Context, 2),
	}
	body := []byte(`{"schemaVersion":2}`)
	envelope := struct {
		Record meta.ManifestRecord `json:"record"`
		Body   []byte              `json:"body"`
	}{
		Record: meta.ManifestRecord{
			Digest: testDigestFor(body), MediaType: distribution.MediaTypeOCIManifest, Size: int64(len(body)),
		},
		Body: body,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	key := "manifest:hub:library/alpine:latest:" + reference.AcceptKey("")
	if err := backendCache.Set(t.Context(), key, encoded, time.Minute); err != nil {
		t.Fatal(err)
	}
	proxy := newTestProxy(nil, metadata, objects, backendCache, config.Config{})
	service, ok := proxy.Manifests().(cache.ManifestPreparer)
	if !ok {
		t.Fatal("production manifest proxy does not implement ManifestPreparer")
	}
	return service, metadata, backendCache, body
}

type manifestCompletionContextKey struct{}

type manifestPreparationMetadata struct {
	meta.Store
	pulls       atomic.Int64
	touches     atomic.Int64
	badContexts atomic.Int64
}

func (s *manifestPreparationMetadata) observeContext(ctx context.Context) {
	if ctx.Err() != nil || ctx.Value(manifestCompletionContextKey{}) != "completion" {
		s.badContexts.Add(1)
	}
}

func (s *manifestPreparationMetadata) RecordPull(ctx context.Context, key meta.PullKey, at time.Time) (*meta.PullRecord, error) {
	s.pulls.Add(1)
	s.observeContext(ctx)
	pull, err := s.Store.RecordPull(ctx, key, at)
	if err != nil {
		return nil, fmt.Errorf("record prepared manifest pull: %w", err)
	}
	return pull, nil
}

func (s *manifestPreparationMetadata) Blob(ctx context.Context, key meta.BlobKey) (*meta.BlobRecord, bool, error) {
	s.touches.Add(1)
	s.observeContext(ctx)
	blob, found, err := s.Store.Blob(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("lookup prepared manifest blob: %w", err)
	}
	return blob, found, nil
}

func assertPreparedManifestPulls(t *testing.T, metadata *manifestPreparationMetadata, want int64) {
	t.Helper()
	if metadata.badContexts.Load() != 0 {
		t.Errorf("completion metadata calls with expired or incorrect context=%d", metadata.badContexts.Load())
	}
	if metadata.pulls.Load() != want {
		t.Errorf("completion pull calls=%d, want %d", metadata.pulls.Load(), want)
	}
	key := meta.PullKey{Alias: "hub", Repository: "library/alpine", Reference: "latest"}
	pull, found, err := metadata.Pull(t.Context(), key)
	if err != nil || !found {
		t.Fatalf("read completed pull: found=%t error=%v", found, err)
	}
	if pull.Count != want {
		t.Errorf("persisted completion pulls=%d, want %d", pull.Count, want)
	}
}

type manifestPreparationBackend struct {
	backend.Backend
	entered chan context.Context
	gate    <-chan struct{}
	reads   atomic.Int64
}

func (b *manifestPreparationBackend) Get(ctx context.Context, key string) ([]byte, bool, error) {
	b.reads.Add(1)
	b.entered <- ctx
	if b.gate != nil {
		select {
		case <-b.gate:
		case <-ctx.Done():
			return nil, false, fmt.Errorf("wait for prepared manifest cache: %w", ctx.Err())
		}
	}
	data, found, err := b.Backend.Get(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("read prepared manifest cache: %w", err)
	}
	return data, found, nil
}

type manifestPreparationResult struct {
	manifest *cache.CachedManifest
	complete cache.ManifestCompletion
	err      error
}

func receiveManifestPreparation(t *testing.T, results <-chan manifestPreparationResult) manifestPreparationResult {
	t.Helper()
	select {
	case result := <-results:
		if result.err != nil || result.manifest == nil || result.complete == nil {
			t.Fatalf("prepared manifest=%v completion=%t error=%v", result.manifest, result.complete != nil, result.err)
		}
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("manifest preparation did not finish")
		return manifestPreparationResult{}
	}
}
