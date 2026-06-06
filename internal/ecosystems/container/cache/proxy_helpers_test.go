package cache_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func newTestStores(t *testing.T) (meta.Store, object.Store) {
	t.Helper()
	metadata, err := meta.OpenSQLite(t.TempDir()+"/regimux.db", nil)
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Fatalf("close metadata store: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open object store: %v", err)
	}
	return metadata, objects
}

func readAndClose(t *testing.T, reader io.ReadCloser) []byte {
	t.Helper()
	body, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close reader: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}

func assertRangeBlobBypass(t *testing.T, result *cache.BlobReadResult) {
	t.Helper()
	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheBypass || result.Status != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("unexpected range result: cache=%s status=%d body=%q", result.Cache, result.Status, body)
	}
	if got := result.Headers.Get(distribution.HeaderContentRange); got != "bytes 2-5/10" {
		t.Fatalf("unexpected content range %q", got)
	}
	if got := result.Headers.Get(distribution.HeaderContentLength); got != "4" {
		t.Fatalf("unexpected content length %q", got)
	}
}

func assertRangeBlobHit(t *testing.T, result *cache.BlobReadResult) {
	t.Helper()
	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheHit || result.Status != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("unexpected range result: cache=%s status=%d body=%q", result.Cache, result.Status, body)
	}
	if got := result.Headers.Get(distribution.HeaderContentRange); got != "bytes 2-5/10" {
		t.Fatalf("unexpected content range %q", got)
	}
	if got := result.Headers.Get(distribution.HeaderContentLength); got != "4" {
		t.Fatalf("unexpected content length %q", got)
	}
}

func assertFullBlobHit(t *testing.T, result *cache.BlobReadResult, want []byte) {
	t.Helper()
	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheHit || !bytes.Equal(body, want) {
		t.Fatalf("unexpected full result: cache=%s body=%q", result.Cache, body)
	}
}

func assertFullBlobMiss(t *testing.T, result *cache.BlobReadResult, want []byte) {
	t.Helper()
	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheMiss || !bytes.Equal(body, want) {
		t.Fatalf("unexpected full result: cache=%s body=%q", result.Cache, body)
	}
}

func assertHeadBlobHit(t *testing.T, result *cache.BlobReadResult, wantSize int) {
	t.Helper()
	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheHit || len(body) != 0 {
		t.Fatalf("unexpected head result: cache=%s body=%q", result.Cache, body)
	}
	if got := result.Headers.Get(distribution.HeaderContentLength); got != strconv.Itoa(wantSize) {
		t.Fatalf("unexpected head content length %q", got)
	}
}

func assertBlobAccessTouched(ctx context.Context, t *testing.T, metadata meta.Store, digest string, old time.Time) {
	t.Helper()

	blob := requireBlobRecord(ctx, t, metadata, digest)
	if !blob.LastAccessAt.After(old) {
		t.Fatalf("blob access was not touched: old=%s got=%s", old, blob.LastAccessAt)
	}
	repoBlob := requireRepoBlobRecord(ctx, t, metadata, digest)
	if !repoBlob.LastAccessAt.After(old) {
		t.Fatalf("repo blob access was not touched: old=%s got=%s", old, repoBlob.LastAccessAt)
	}
	if !repoBlob.LastVerifiedAt.Equal(old) {
		t.Fatalf("repo blob verification time changed: old=%s got=%s", old, repoBlob.LastVerifiedAt)
	}
}

func requireBlobRecord(ctx context.Context, t *testing.T, metadata meta.Store, digest string) *meta.BlobRecord {
	t.Helper()

	blob, ok, err := metadata.Blob(ctx, meta.BlobKey{Digest: digest})
	if err != nil || !ok {
		t.Fatalf("blob metadata lookup: ok=%v err=%v", ok, err)
	}
	return blob
}

func requireRepoBlobRecord(ctx context.Context, t *testing.T, metadata meta.Store, digest string) *meta.RepoBlobRecord {
	t.Helper()

	repoBlob, ok, err := metadata.RepoBlob(ctx, meta.RepoBlobKey{
		Alias:      "hub",
		Repository: "library/alpine",
		Digest:     digest,
	})
	if err != nil || !ok {
		t.Fatalf("repo blob metadata lookup: ok=%v err=%v", ok, err)
	}
	return repoBlob
}

func setRepoBlobVerifiedAt(ctx context.Context, t *testing.T, metadata meta.Store, digest string, verifiedAt time.Time) {
	t.Helper()

	record := requireRepoBlobRecord(ctx, t, metadata, digest)
	record.LastVerifiedAt = verifiedAt
	_, err := metadata.UpsertRepoBlob(ctx, *record)
	if err != nil {
		t.Fatalf("update repo blob verify time: %v", err)
	}
}

func assertObjectPresence(ctx context.Context, t *testing.T, objects object.Store, digest string, want bool) {
	t.Helper()

	exists, err := objects.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("check object exists: %v", err)
	}
	if exists != want {
		t.Fatalf("object presence for %s = %v, want %v", digest, exists, want)
	}
}

func waitObjectStored(ctx context.Context, t *testing.T, objects object.Store, digest string) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for {
		exists, err := objects.Exists(ctx, digest)
		if err != nil {
			t.Fatalf("check object exists: %v", err)
		}
		if exists {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("object presence for %s did not become true", digest)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertBlobRequestCounters(t *testing.T, client *fakeRegistryClient, gets, heads int) {
	t.Helper()

	if client.blobGets != gets || client.blobHeads != heads {
		t.Fatalf("unexpected blob request counters: gets=%d heads=%d", client.blobGets, client.blobHeads)
	}
}

func assertPullRecordState(t *testing.T, pull *meta.PullRecord, count int64, upstreamTime time.Time) {
	t.Helper()

	if pull.Count != count || pull.LastPullAt.IsZero() || pull.LastUpstreamPullAt.IsZero() {
		t.Fatalf("unexpected pull record: %#v", pull)
	}
	if !upstreamTime.IsZero() && !pull.LastUpstreamPullAt.Equal(upstreamTime) {
		t.Fatalf("unexpected upstream pull time: %#v", pull)
	}
}

func requirePullRecord(ctx context.Context, t *testing.T, metadata meta.Store, key meta.PullKey) *meta.PullRecord {
	t.Helper()

	pull, ok, err := metadata.Pull(ctx, key)
	if err != nil || !ok {
		t.Fatalf("pull lookup: ok=%v err=%v", ok, err)
	}
	return pull
}

func testDigestFor(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type blockingBlobReader struct {
	mu       sync.Mutex
	reader   *bytes.Reader
	released chan struct{}
	closed   bool
}

func newBlockingBlobReader(body []byte) *blockingBlobReader {
	return &blockingBlobReader{
		reader:   bytes.NewReader(body),
		released: make(chan struct{}),
	}
}

func (r *blockingBlobReader) Read(p []byte) (int, error) {
	<-r.released
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	return r.read(p)
}

func (r *blockingBlobReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

func (r *blockingBlobReader) Release() {
	close(r.released)
}

func (r *blockingBlobReader) read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == nil {
		return n, nil
	}
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	return n, fmt.Errorf("read blocking blob: %w", err)
}
