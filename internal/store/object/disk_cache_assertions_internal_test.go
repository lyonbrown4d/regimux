package object

import (
	"bytes"
	"testing"
)

func readCachedObject(t *testing.T, cache *DiskCacheStore, digest string, want []byte) {
	t.Helper()
	reader, _, err := cache.Get(t.Context(), digest, GetOptions{})
	if err != nil {
		t.Fatalf("get full object: %v", err)
	}
	if got := readDiskCacheBody(t, reader); !bytes.Equal(got, want) {
		t.Fatalf("full object = %q, want %q", got, want)
	}
}

func readDiskCacheRange(t *testing.T, cache *DiskCacheStore, digest string, requested *HTTPRange, want []byte) {
	t.Helper()
	reader, info, err := cache.Get(t.Context(), digest, GetOptions{Range: requested})
	if err != nil {
		t.Fatalf("get range: %v", err)
	}
	if info.Size != int64(len(want)) {
		t.Fatalf("range info size = %d, want %d", info.Size, len(want))
	}
	if got := readDiskCacheBody(t, reader); !bytes.Equal(got, want) {
		t.Fatalf("range body = %q, want %q", got, want)
	}
}

func newTestDiskCache(t *testing.T, backend Store, opts DiskCacheOptions) *DiskCacheStore {
	t.Helper()
	cache, err := NewDiskCache(backend, opts)
	if err != nil {
		t.Fatalf("create disk cache: %v", err)
	}
	t.Cleanup(func() { closeDiskCache(t, cache) })
	return cache
}

func closeDiskCache(t *testing.T, cache *DiskCacheStore) {
	t.Helper()
	if err := cache.Close(); err != nil {
		t.Errorf("close disk cache: %v", err)
	}
}
