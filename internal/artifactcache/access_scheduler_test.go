package artifactcache_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/panjf2000/ants/v2"
	"github.com/stretchr/testify/require"
)

func TestLookupManifestReturnsBeforeBlockedAccessTouch(t *testing.T) {
	metadata, objects := newBlockedAccessFixture(t)
	pool := newAccessTestPool(t, 1)
	t.Cleanup(metadata.releaseTouch)
	touchDone := make(chan struct{})
	var submits atomic.Int64
	store := newAccessTestStore(metadata, objects, func(task func()) error {
		submits.Add(1)
		return submitAccessTestTask(pool, func() {
			defer close(touchDone)
			task()
		})
	})

	ctx, cancel := context.WithTimeout(t.Context(), 125*time.Millisecond)
	defer cancel()
	manifest, found, err := artifactcache.LookupManifest(ctx, store, accessTestKey("short-deadline"))
	require.NoError(t, err)
	assertAccessManifest(t, manifest, found, metadata.manifest.Digest)
	require.NoError(t, ctx.Err(), "metadata must return before the caller deadline")
	waitAccessSignal(t, metadata.touchEntered, "background access touch")
	require.EqualValues(t, 1, submits.Load())
	require.EqualValues(t, 1, metadata.touchCalls.Load())

	cancel()
	metadata.releaseTouch()
	waitAccessSignal(t, touchDone, "released access touch")
	require.True(t, metadata.accessCompleted.Load(), "a completed read must retain its bounded access update after caller cancellation")
}

func TestLookupManifestDeduplicatesPendingTouchesByDigest(t *testing.T) {
	metadata, objects := newBlockedAccessFixture(t)
	pool := newAccessTestPool(t, 128)
	t.Cleanup(metadata.releaseTouch)
	var submits atomic.Int64
	store := newAccessTestStore(metadata, objects, func(task func()) error {
		submits.Add(1)
		return submitAccessTestTask(pool, task)
	})

	manifest, found, err := artifactcache.LookupManifest(t.Context(), store, accessTestKey("owner"))
	require.NoError(t, err)
	assertAccessManifest(t, manifest, found, metadata.manifest.Digest)
	waitAccessSignal(t, metadata.touchEntered, "first pending access touch")

	const readers = 128
	results := make(chan manifestReadResult, readers-1)
	for index := 1; index < readers; index++ {
		key := accessTestKey(fmt.Sprintf("reader-%d", index))
		go func() {
			manifest, found, lookupErr := artifactcache.LookupManifest(t.Context(), store, key)
			results <- manifestReadResult{manifest: manifest, found: found, err: lookupErr}
		}()
	}
	for range readers - 1 {
		result := <-results
		require.NoError(t, result.err)
		assertAccessManifest(t, result.manifest, result.found, metadata.manifest.Digest)
	}
	require.EqualValues(t, 1, submits.Load(), "pending work for one digest must be submitted once")
	require.EqualValues(t, 1, metadata.touchCalls.Load(), "one background touch should serve all metadata reads")

	metadata.releaseTouch()
	waitAccessSignal(t, metadata.touchFinished, "released shared access touch")
}

func TestLookupManifestIgnoresAccessSubmitRejectionAndRetries(t *testing.T) {
	metadata, objects := newReadMetadata(time.Now().Add(-time.Hour), []byte("artifact")), &readObjectStore{body: []byte("artifact")}
	close(metadata.releaseTag)
	pool := newAccessTestPool(t, 1)
	touchDone := make(chan struct{})
	var submits atomic.Int64
	store := newAccessTestStore(metadata, objects, func(task func()) error {
		if submits.Add(1) == 1 {
			return ants.ErrPoolOverload
		}
		return submitAccessTestTask(pool, func() {
			defer close(touchDone)
			task()
		})
	})
	key := accessTestKey("retry-after-rejection")

	manifest, found, err := artifactcache.LookupManifest(t.Context(), store, key)
	require.NoError(t, err, "best-effort scheduler rejection must not hide metadata")
	assertAccessManifest(t, manifest, found, metadata.manifest.Digest)
	manifest, found, err = artifactcache.LookupManifest(t.Context(), store, key)
	require.NoError(t, err, "a rejected submission must not leave the digest marked pending")
	assertAccessManifest(t, manifest, found, metadata.manifest.Digest)
	require.EqualValues(t, 2, submits.Load())
	waitAccessSignal(t, touchDone, "retry access touch")
	require.EqualValues(t, 1, metadata.touchCalls.Load())
}

func TestLookupManifestCanceledCallerDoesNotSubmitTouch(t *testing.T) {
	metadata, objects := newReadMetadata(time.Now().Add(-time.Hour), []byte("artifact")), &readObjectStore{body: []byte("artifact")}
	pool := newAccessTestPool(t, 1)
	var submits atomic.Int64
	store := newAccessTestStore(metadata, objects, func(task func()) error {
		submits.Add(1)
		return submitAccessTestTask(pool, task)
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, found, err := artifactcache.LookupManifest(ctx, store, accessTestKey("canceled"))
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, found)
	require.Zero(t, submits.Load(), "canceled reads must not enqueue best-effort work")
}

func newBlockedAccessFixture(t *testing.T) (*blockedAccessMetadata, *readObjectStore) {
	t.Helper()
	body := []byte("complete artifact body")
	metadata := &blockedAccessMetadata{
		readMetadata:  newReadMetadata(time.Now().Add(-time.Hour), body),
		touchEntered:  make(chan struct{}),
		touchRelease:  make(chan struct{}),
		touchFinished: make(chan struct{}),
	}
	close(metadata.releaseTag)
	return metadata, &readObjectStore{body: body}
}

type blockedAccessMetadata struct {
	*readMetadata
	touchEntered    chan struct{}
	touchRelease    chan struct{}
	touchFinished   chan struct{}
	enteredOnce     sync.Once
	finishedOnce    sync.Once
	releaseOnce     sync.Once
	accessCompleted atomic.Bool
}

func (m *blockedAccessMetadata) TouchBlob(ctx context.Context, key meta.BlobKey, at time.Time) error {
	m.touchCalls.Add(1)
	m.enteredOnce.Do(func() { close(m.touchEntered) })
	defer func() { m.finishedOnce.Do(func() { close(m.touchFinished) }) }()
	select {
	case <-m.touchRelease:
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("complete cache access update: %w", err)
		}
		m.accessCompleted.Store(true)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for cache access update: %w", ctx.Err())
	}
}

func (m *blockedAccessMetadata) releaseTouch() {
	m.releaseOnce.Do(func() { close(m.touchRelease) })
}

func newAccessTestPool(t *testing.T, capacity int) *ants.Pool {
	t.Helper()
	pool, err := ants.NewPool(capacity, ants.WithNonblocking(true))
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := pool.ReleaseTimeout(3 * time.Second); err != nil {
			t.Errorf("release access test pool: %v", err)
		}
	})
	return pool
}

func newAccessTestStore(metadata artifactcache.MetadataStore, objects object.Store, submit func(func()) error) *artifactcache.Store {
	return artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects, SubmitAccess: submit})
}

func submitAccessTestTask(pool *ants.Pool, task func()) error {
	if err := pool.Submit(task); err != nil {
		return fmt.Errorf("submit access test task: %w", err)
	}
	return nil
}

func accessTestKey(reference string) artifactcache.Key {
	return artifactcache.Key{Alias: "pypi", Repository: "packages/demo", Reference: reference}
}

func assertAccessManifest(t *testing.T, manifest *meta.ManifestRecord, found bool, digest string) {
	t.Helper()
	require.True(t, found)
	require.NotNil(t, manifest)
	require.Equal(t, digest, manifest.Digest)
	require.Equal(t, []string{"first", "second"}, manifest.Headers["X-Test"])
}

func waitAccessSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
