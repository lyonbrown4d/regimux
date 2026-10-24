package object

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiskCacheReadThroughPersistsAcrossRestart(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	opts := diskCacheTestOptions(t)
	cache, err := NewDiskCache(backend, opts)
	require.NoError(t, err, "create disk cache")
	t.Cleanup(func() { closeDiskCache(t, cache) })
	_, reopenErr := NewDiskCache(backend, opts)
	require.Error(t, reopenErr, "opening an already locked cache directory")

	readCachedObject(t, cache, digest, payload)
	readCachedObject(t, cache, digest, payload)
	require.EqualValues(t, 1, backend.gets.Load(), "backend Get calls after warm read")
	require.Equal(t, DiskCacheStats{Entries: 1, Bytes: int64(len(payload))}, cache.Stats(), "cache stats after fill")
	require.NoError(t, cache.Close(), "close disk cache")

	reopened, err := NewDiskCache(backend, opts)
	require.NoError(t, err, "reopen disk cache")
	defer closeDiskCache(t, reopened)
	readCachedObject(t, reopened, digest, payload)
	require.EqualValues(t, 1, backend.gets.Load(), "backend Get calls after restart")
}

func TestDiskCacheRangeMissDoesNotFillButRangeHitUsesCache(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))

	ranged := &HTTPRange{Start: 2, End: 7}
	readDiskCacheRange(t, cache, digest, ranged, payload[2:8])
	require.Zero(t, cache.Stats().Entries, "range miss must not populate cache")
	readCachedObject(t, cache, digest, payload)
	readDiskCacheRange(t, cache, digest, ranged, payload[2:8])
	require.EqualValues(t, 2, backend.gets.Load(), "backend Get calls after range miss/fill/hit")
}

func TestDiskCacheDoesNotPublishCorruptBackendBody(t *testing.T) {
	backend, digest, _ := newDiskCacheTestBackend(t)
	backend.get = func(ctx context.Context, requested string, opts GetOptions) (io.ReadCloser, *Info, error) {
		reader, info, err := backend.getCanonical(ctx, requested, opts)
		if err != nil {
			return nil, nil, err
		}
		if closeErr := reader.Close(); closeErr != nil {
			return nil, nil, fmt.Errorf("close seeded canonical reader: %w", closeErr)
		}
		return io.NopCloser(bytes.NewReader([]byte("bad body"))), info, nil
	}
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))

	reader, _, err := cache.Get(t.Context(), digest, GetOptions{})
	if err != nil {
		require.ErrorIs(t, err, ErrDigestMismatch, "opening corrupt backend body")
	} else {
		_, readErr := io.ReadAll(reader)
		require.ErrorIs(t, readErr, ErrDigestMismatch, "reading corrupt backend body")
		require.NoError(t, reader.Close(), "close corrupt body reader")
	}
	require.Zero(t, cache.Stats().Entries, "corrupt object must not be published")
	require.Zero(t, cache.Stats().Fills, "corrupt fill must be cleaned")
	require.Zero(t, cache.Stats().ReservedBytes, "corrupt fill reservation must be released")
	assertDiskCacheTemporaryDirectoryEmpty(t, cache)
}

func TestDiskCacheEarlyCloseAndOversizeDoNotPublish(t *testing.T) {
	t.Run("early close", func(t *testing.T) {
		backend, digest, _ := newDiskCacheTestBackend(t)
		cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
		reader, _, err := cache.Get(t.Context(), digest, GetOptions{})
		require.NoError(t, err, "get object")
		one := make([]byte, 1)
		_, readErr := reader.Read(one)
		require.NoError(t, readErr, "read partial object")
		require.NoError(t, reader.Close(), "close early reader")
		require.Zero(t, cache.Stats().Entries, "early close must not publish object")
	})

	t.Run("oversize bypass", func(t *testing.T) {
		backend, digest, payload := newDiskCacheTestBackend(t)
		opts := diskCacheTestOptions(t)
		opts.MaxObjectBytes = 4
		cache := newTestDiskCache(t, backend, opts)
		readCachedObject(t, cache, digest, payload)
		require.Zero(t, cache.Stats().Entries, "oversize object must not populate cache")
	})
}

func TestDiskCacheByteQuotaBypassesObjectLargerThanCache(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	opts := diskCacheTestOptions(t)
	opts.MaxBytes = int64(len(payload) - 1)
	opts.MaxObjectBytes = opts.MaxBytes
	cache := newTestDiskCache(t, backend, opts)
	readCachedObject(t, cache, digest, payload)
	require.Equal(t, DiskCacheStats{}, cache.Stats(), "object larger than byte quota must not be retained")
}

func TestDiskCacheRestartEnforcesSmallerConfiguredCapacity(t *testing.T) {
	backend, firstDigest, first := newDiskCacheTestBackend(t)
	second := []byte("second object for restart capacity")
	secondDigest := digestForTest(second)
	_, err := backend.canonical.Put(t.Context(), secondDigest, bytes.NewReader(second), PutOptions{})
	require.NoError(t, err, "seed second object")
	opts := diskCacheTestOptions(t)
	opts.MaxEntries = 2
	cache := newTestDiskCache(t, backend, opts)
	readCachedObject(t, cache, firstDigest, first)
	readCachedObject(t, cache, secondDigest, second)
	require.Equal(t, 2, cache.Stats().Entries, "pre-restart entries")
	require.NoError(t, cache.Close(), "close initial disk cache")

	opts.MaxEntries = 1
	reopened, err := NewDiskCache(backend, opts)
	require.NoError(t, err, "reopen with smaller capacity")
	defer closeDiskCache(t, reopened)
	stats := reopened.Stats()
	require.LessOrEqual(t, stats.Entries, opts.MaxEntries, "recovered entry count")
	require.LessOrEqual(t, stats.Bytes, opts.MaxBytes, "recovered byte count")
	listed, err := reopened.ListObjects(t.Context())
	require.NoError(t, err, "canonical ListObjects after cache trim")
	require.Len(t, listed, 2, "cache recovery must preserve canonical listing")
}
