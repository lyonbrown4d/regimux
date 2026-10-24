package object

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiskCachePinnedEntryIsNotReplacedAfterEvict(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	readCachedObject(t, cache, digest, payload)
	pinned, _, err := cache.Get(t.Context(), digest, GetOptions{})
	require.NoError(t, err, "pin warm object")
	require.NoError(t, cache.Evict(t.Context(), digest), "evict pinned object")
	require.Zero(t, cache.Stats().Entries, "pinned eviction entry count")
	require.EqualValues(t, len(payload), cache.Stats().Bytes, "pinned eviction byte accounting")

	readCachedObject(t, cache, digest, payload)
	require.Equal(t, 0, cache.Stats().Fills, "new fill must not replace a still-pinned entry")
	require.Zero(t, cache.Stats().Entries, "pinned entry remains unavailable to new readers")
	require.Equal(t, payload, readDiskCacheBody(t, pinned), "old pinned reader remains usable")
	readCachedObject(t, cache, digest, payload)
	require.EqualValues(t, 3, backend.gets.Load(), "backend Gets across pin/evict/re-fill")
	require.Equal(t, 1, cache.Stats().Entries, "cache recovers one entry after pin release")
	require.EqualValues(t, len(payload), cache.Stats().Bytes, "cache recovers byte accounting after pin release")
}

func TestDiskCacheCloseWithPinnedReaderCanReopenSameDirectory(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	opts := diskCacheTestOptions(t)
	cache, err := NewDiskCache(backend, opts)
	require.NoError(t, err, "create disk cache")
	t.Cleanup(func() { closeDiskCache(t, cache) })
	readCachedObject(t, cache, digest, payload)
	oldReader, _, err := cache.Get(t.Context(), digest, GetOptions{})
	require.NoError(t, err, "open pinned object")
	require.NoError(t, cache.Close(), "close disk cache with reader")
	_, readErr := oldReader.Read(make([]byte, 1))
	require.Error(t, readErr, "reader must stop after owning cache Close")
	require.NoError(t, oldReader.Close(), "idempotent close of old reader")

	reopened, err := NewDiskCache(backend, opts)
	require.NoError(t, err, "reopen disk cache")
	defer closeDiskCache(t, reopened)
	readCachedObject(t, reopened, digest, payload)
	require.EqualValues(t, 1, backend.gets.Load(), "backend Get calls after reader Close/reopen")
}

func TestDiskCacheFillAdmissionDoesNotBlockConcurrentMisses(t *testing.T) {
	backend, digest, payload, otherDigest, other := seedDiskCacheAdmissionObjects(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	var gateFirst atomic.Bool
	backend.get = gatedFirstDiskCacheGet(backend, digest, started, release, &gateFirst)
	cacheOptions := diskCacheTestOptions(t)
	cacheOptions.MaxConcurrentFills = 1
	cache := newTestDiskCache(t, backend, cacheOptions)
	first, _, err := cache.Get(t.Context(), digest, GetOptions{})
	require.NoError(t, err, "open first miss")
	firstDone := make(chan error, 1)
	go func() { firstDone <- readAndCloseDiskCacheReader(first, payload) }()
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	waitForDiskCacheSignal(t, started)
	require.Equal(t, 1, cache.Stats().Fills, "active fill count")
	require.EqualValues(t, len(payload), cache.Stats().ReservedBytes, "active fill reservation")

	shared, _, err := cache.Get(t.Context(), digest, GetOptions{})
	require.NoError(t, err, "open same-key shared miss")
	sharedDone := make(chan error, 1)
	go func() { sharedDone <- readAndCloseDiskCacheReader(shared, payload) }()
	assertDiskCacheBypassRead(t, cache, otherDigest, other, "different-key concurrent miss")
	require.Equal(t, 1, cache.Stats().Fills, "concurrent misses must respect fill admission")
	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-firstDone, "finish first fill")
	require.NoError(t, <-sharedDone, "finish same-key shared reader")
	require.EqualValues(t, 2, backend.gets.Load(), "canonical Gets for concurrent misses")
}

func seedDiskCacheAdmissionObjects(t *testing.T) (*diskCacheTestBackend, string, []byte, string, []byte) {
	t.Helper()
	backend, digest, payload := newDiskCacheTestBackend(t)
	other := []byte("different key streamed while a fill is active")
	otherDigest := digestForTest(other)
	_, err := backend.canonical.Put(t.Context(), otherDigest, bytes.NewReader(other), PutOptions{})
	require.NoError(t, err, "seed second object")
	return backend, digest, payload, otherDigest, other
}

func gatedFirstDiskCacheGet(
	backend *diskCacheTestBackend,
	digest string,
	started chan<- struct{},
	release <-chan struct{},
	gateFirst *atomic.Bool,
) func(context.Context, string, GetOptions) (io.ReadCloser, *Info, error) {
	return func(ctx context.Context, requested string, opts GetOptions) (io.ReadCloser, *Info, error) {
		reader, info, err := backend.getCanonical(ctx, requested, opts)
		if err != nil {
			return nil, nil, err
		}
		if requested == digest && gateFirst.CompareAndSwap(false, true) {
			return &diskCacheGateReader{ctx: ctx, source: reader, started: started, release: release}, info, nil
		}
		return reader, info, nil
	}
}

func assertDiskCacheBypassRead(t *testing.T, cache *DiskCacheStore, digest string, want []byte, scenario string) {
	t.Helper()
	reader, _, err := cache.Get(t.Context(), digest, GetOptions{})
	require.NoError(t, err, "open %s", scenario)
	require.Equal(t, want, readDiskCacheBody(t, reader), "%s body", scenario)
}

func readAndCloseDiskCacheReader(reader io.ReadCloser, want []byte) error {
	data, readErr := io.ReadAll(reader)
	if readErr == nil && !bytes.Equal(data, want) {
		readErr = fmt.Errorf("body = %q, want %q: %w", data, want, io.ErrUnexpectedEOF)
	}
	closeErr := reader.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close completed disk cache reader: %w", closeErr)
	}
	return errors.Join(readErr, closeErr)
}

func TestDiskCacheTempCreationFailurePreservesCanonicalRead(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	require.NoError(t, os.Remove(cache.tempDir), "remove fill directory")
	require.NoError(t, os.WriteFile(cache.tempDir, []byte("not a directory"), 0o600), "block fill directory")
	readCachedObject(t, cache, digest, payload)
	stats := cache.Stats()
	require.Zero(t, stats.Entries, "temporary-file failure entry count")
	require.Zero(t, stats.Fills, "temporary-file failure fill count")
	require.Zero(t, stats.ReservedBytes, "temporary-file failure reservation")
	require.EqualValues(t, 1, backend.gets.Load(), "canonical Get calls after temp creation failure")
}

func TestDiskCacheRecoversDirectoryContainingOnlyLockFile(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	opts := diskCacheTestOptions(t)
	require.NoError(t, os.MkdirAll(opts.Directory, 0o700), "create lock-only directory")
	lockPath := filepath.Join(opts.Directory, ".disk-cache.lock")
	require.NoError(t, os.WriteFile(lockPath, nil, 0o600), "create leftover lock file")

	cache, err := NewDiskCache(backend, opts)
	require.NoError(t, err, "recover lock-only cache directory")
	defer closeDiskCache(t, cache)
	readCachedObject(t, cache, digest, payload)
	require.Equal(t, 1, cache.Stats().Entries, "recovered cache entry")
}
