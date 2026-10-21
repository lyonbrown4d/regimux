package cache

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	ocidigest "github.com/opencontainers/go-digest"
	"golang.org/x/sync/semaphore"
)

func TestSmallBlobCacheLookupSpoolsBodyAndReleasesSlot(t *testing.T) {
	ctx := context.Background()
	body := []byte("cached body")
	digest := smallBlobTestDigest(body)
	encoded, err := encodeSmallBlobCache(digest, "application/octet-stream", body)
	if err != nil {
		t.Fatal(err)
	}
	cache := &smallBlobAdmissionTestBackend{value: encoded, found: true}
	gate := semaphore.NewWeighted(4)
	proxy := blobProxy{
		cache:         cache,
		smallCache:    smallBlobCacheConfig{enabled: true, maxSizeBytes: 1024, ttl: time.Minute},
		smallBlobGate: gate,
	}
	t.Setenv("TMPDIR", t.TempDir())

	result, ok, err := proxy.lookupSmallBlobCache(ctx, BlobRequest{Digest: digest, Method: http.MethodGet})
	if err != nil || !ok {
		t.Fatalf("small cache lookup: hit=%v error=%v", ok, err)
	}
	assertSmallCacheAdmissionReleased(t, gate)
	assertSmallCacheSpoolFiles(t, 1, "before close")
	got, readErr := io.ReadAll(result.Reader)
	closeErr := result.Reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, body) {
		t.Fatalf("spooled body = %q, read error=%v close error=%v", got, readErr, closeErr)
	}
	assertSmallCacheSpoolFiles(t, 0, "after close")
}

func assertSmallCacheAdmissionReleased(t *testing.T, gate *semaphore.Weighted) {
	t.Helper()
	if !gate.TryAcquire(4) {
		t.Fatal("cache lookup retained an admission slot")
	}
	gate.Release(4)
}

func assertSmallCacheSpoolFiles(t *testing.T, want int, stage string) {
	t.Helper()
	entries, err := os.ReadDir(os.Getenv("TMPDIR"))
	if err != nil || len(entries) != want {
		t.Fatalf("spooled body files %s: entries=%d error=%v", stage, len(entries), err)
	}
}

func TestSmallBlobCacheLookupBypassesWhenAllSlotsAreBusy(t *testing.T) {
	ctx := context.Background()
	cache := &smallBlobAdmissionTestBackend{found: true}
	gate := semaphore.NewWeighted(4)
	if err := gate.Acquire(ctx, 4); err != nil {
		t.Fatal(err)
	}
	defer gate.Release(4)
	proxy := blobProxy{
		cache:         cache,
		smallCache:    smallBlobCacheConfig{enabled: true, maxSizeBytes: 1024},
		smallBlobGate: gate,
	}

	result, ok, err := proxy.lookupSmallBlobCache(ctx, BlobRequest{Digest: "sha256:test", Method: http.MethodGet})
	if result != nil || ok || err != nil {
		t.Fatalf("busy cache lookup = (%v, %v, %v), want miss without error", result, ok, err)
	}
	if cache.gets != 0 {
		t.Fatalf("busy cache lookup made %d backend calls, want bypass before Get", cache.gets)
	}
}

func TestSmallBlobRecorderDropsBufferAndReleasesSlotOnOverflow(t *testing.T) {
	gate := semaphore.NewWeighted(1)
	recorder := newSmallBlobRecorder(3, gate)
	if recorder == nil {
		t.Fatal("recorder did not acquire its slot")
	}
	if _, err := recorder.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if gate.TryAcquire(1) {
		gate.Release(1)
		t.Fatal("recording released its slot before upload completed")
	}
	if _, err := recorder.Write([]byte("d")); err != nil {
		t.Fatal(err)
	}
	if !recorder.overflow || recorder.buffer.Cap() != 0 {
		t.Fatalf("overflow recorder retained bytes: overflow=%v capacity=%d", recorder.overflow, recorder.buffer.Cap())
	}
	if !gate.TryAcquire(1) {
		t.Fatal("overflow did not release its slot")
	}
	gate.Release(1)
	if _, err := recorder.Write(bytes.Repeat([]byte("x"), 1024)); err != nil {
		t.Fatal(err)
	}
	if recorder.buffer.Cap() != 0 {
		t.Fatalf("overflow recorder resumed buffering with capacity %d", recorder.buffer.Cap())
	}
}

type smallBlobAdmissionTestBackend struct {
	value []byte
	found bool
	gets  int
}

func (b *smallBlobAdmissionTestBackend) Get(context.Context, string) ([]byte, bool, error) {
	b.gets++
	return bytes.Clone(b.value), b.found, nil
}

func (*smallBlobAdmissionTestBackend) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (*smallBlobAdmissionTestBackend) Delete(context.Context, string) error { return nil }
func (*smallBlobAdmissionTestBackend) Close() error                         { return nil }

func smallBlobTestDigest(body []byte) string {
	return ocidigest.FromBytes(body).String()
}

var _ backend.Backend = (*smallBlobAdmissionTestBackend)(nil)
