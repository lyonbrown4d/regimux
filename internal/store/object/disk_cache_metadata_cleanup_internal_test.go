package object

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiskCacheMetadataRemovalFailureKeepsEntryForRetry(t *testing.T) {
	fixture := seedDiskCacheMetadataRetry(t)

	fixture.cache.mu.Lock()
	fixture.entry.metaPath = filepath.Join(fixture.originalMetaPath, "blocked-child")
	fixture.cache.mu.Unlock()

	err := fixture.cache.Evict(t.Context(), fixture.firstDigest)
	require.Error(t, err, "regular metadata parent makes child lstat fail with ENOTDIR")
	assertDiskCachePendingTombstoneOccupiesSlot(t, fixture.cache, fixture.firstDigest, fixture.entry)
	require.Zero(t, fixture.cache.Stats().Entries, "pending tombstones are not reported as readable entries")
	require.EqualValues(t, len(fixture.firstPayload), fixture.cache.Stats().Bytes, "failed eviction retains byte accounting")
	_, statErr := os.Stat(filepath.Join(fixture.cache.root, fixture.originalMetaPath))
	require.NoError(t, statErr, "original regular metadata remains for repair")

	readCachedObject(t, fixture.cache, fixture.secondDigest, fixture.secondPayload)
	require.Equal(t, 0, fixture.cache.Stats().Fills, "capacity failure must serve canonical data without filling L1")
	assertDiskCachePendingTombstoneOccupiesSlot(t, fixture.cache, fixture.firstDigest, fixture.entry)
	require.Zero(t, fixture.cache.Stats().Entries, "second digest must not turn pending tombstone into a readable entry")
	require.EqualValues(t, len(fixture.firstPayload), fixture.cache.Stats().Bytes, "second digest must not alter retained accounting")
	require.EqualValues(t, 2, fixture.backend.gets.Load(), "second digest is served by one canonical bypass")

	fixture.cache.mu.Lock()
	fixture.entry.metaPath = fixture.originalMetaPath
	fixture.cache.mu.Unlock()
	require.NoError(t, fixture.cache.Evict(t.Context(), fixture.firstDigest), "retry metadata and body cleanup")
	require.Equal(t, 0, fixture.cache.Stats().Entries, "successful retry removes the tombstone")
	require.Zero(t, fixture.cache.Stats().Bytes, "successful retry releases retained accounting")

	readCachedObject(t, fixture.cache, fixture.secondDigest, fixture.secondPayload)
	require.Equal(t, 1, fixture.cache.Stats().Entries, "cache accepts a fill after cleanup succeeds")
	require.EqualValues(t, len(fixture.secondPayload), fixture.cache.Stats().Bytes, "post-retry fill accounting")
	require.EqualValues(t, 3, fixture.backend.gets.Load(), "second digest is fetched once for bypass and once for fill")
}

type diskCacheMetadataRetryFixture struct {
	backend          *diskCacheTestBackend
	cache            *DiskCacheStore
	firstDigest      string
	firstPayload     []byte
	secondDigest     string
	secondPayload    []byte
	entry            *diskCacheEntry
	originalMetaPath string
}

func seedDiskCacheMetadataRetry(t *testing.T) diskCacheMetadataRetryFixture {
	t.Helper()
	backend, firstDigest, firstPayload := newDiskCacheTestBackend(t)
	secondPayload := []byte("second object must bypass while first metadata is stuck")
	secondDigest := digestForTest(secondPayload)
	_, err := backend.canonical.Put(t.Context(), secondDigest, bytes.NewReader(secondPayload), PutOptions{})
	require.NoError(t, err, "seed second canonical object")
	opts := diskCacheTestOptions(t)
	opts.MaxEntries = 1
	cache := newTestDiskCache(t, backend, opts)
	readCachedObject(t, cache, firstDigest, firstPayload)
	cache.mu.Lock()
	entry := cache.entries[firstDigest]
	if entry == nil {
		cache.mu.Unlock()
		t.Fatal("first object entry missing")
	}
	metaPath := entry.metaPath
	cache.mu.Unlock()
	return diskCacheMetadataRetryFixture{
		backend:          backend,
		cache:            cache,
		firstDigest:      firstDigest,
		firstPayload:     firstPayload,
		secondDigest:     secondDigest,
		secondPayload:    secondPayload,
		entry:            entry,
		originalMetaPath: metaPath,
	}
}

func assertDiskCachePendingTombstoneOccupiesSlot(
	t *testing.T,
	cache *DiskCacheStore,
	digest string,
	want *diskCacheEntry,
) {
	t.Helper()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Same(t, want, cache.entries[digest], "failed metadata cleanup retains the tombstone")
	require.True(t, want.evictPending, "failed metadata cleanup remains pending")
	require.Len(t, cache.entries, 1, "tombstone reserves the entry slot")
	require.Equal(t, 1, cache.lru.Len(), "tombstone remains in LRU capacity accounting")
}
