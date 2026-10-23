package meta

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
)

func TestUpsertBlobSkipsUnchangedContentWrites(t *testing.T) {
	digest := fmt.Sprintf("sha256:%064x", time.Now().UnixNano())
	hook := &blobWriteIdempotencyHook{}
	store := openBlobWriteIdempotencyStore(t, hook)
	lastAccess := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	initial := upsertIdempotencyBlob(t, store, BlobRecord{
		Digest: digest, Size: 42, MediaType: "application/test", ObjectKey: "blobs/original", LastAccessAt: lastAccess,
	})
	hook.armed.Store(true)

	for range 512 {
		upsertIdempotencyBlob(t, store, BlobRecord{
			Digest: digest, Size: 42, MediaType: "application/test", ObjectKey: "blobs/original", LastAccessAt: lastAccess,
		})
	}
	unchanged := readIdempotencyBlob(t, store, digest)
	assertBlobStableIdentity(t, initial, unchanged)
	assertBlobUpdatedAtStable(t, initial, unchanged)
	assertBlobContent(t, unchanged, 42, "application/test", "blobs/original")
	assertContentUpdateStats(t, hook, 512, 0)

	upsertIdempotencyBlob(t, store, BlobRecord{Digest: digest, Size: 43, MediaType: "application/test", ObjectKey: "blobs/original"})
	upsertIdempotencyBlob(t, store, BlobRecord{Digest: digest, Size: 43, MediaType: "application/changed", ObjectKey: "blobs/original"})
	changed := upsertIdempotencyBlob(t, store, BlobRecord{Digest: digest, Size: 43, MediaType: "application/changed", ObjectKey: "blobs/changed"})
	assertBlobStableIdentity(t, initial, changed)
	assertBlobContent(t, changed, 43, "application/changed", "blobs/changed")
	assertContentUpdateStats(t, hook, 515, 3)
	changed = upsertIdempotencyBlob(t, store, BlobRecord{
		Digest: digest, Size: 43, MediaType: "application/changed", ObjectKey: "blobs/Changed",
	})
	assertBlobStableIdentity(t, initial, changed)
	assertBlobContent(t, changed, 43, "application/changed", "blobs/Changed")
	changed = upsertIdempotencyBlob(t, store, BlobRecord{
		Digest: digest, Size: 43, MediaType: "application/changed", ObjectKey: "blobs/Changed ",
	})
	assertBlobStableIdentity(t, initial, changed)
	assertBlobContent(t, changed, 43, "application/changed", "blobs/Changed ")
	changed = upsertIdempotencyBlob(t, store, BlobRecord{
		Digest: digest, Size: 43, MediaType: "Application/Changed", ObjectKey: "blobs/Changed ",
	})
	assertBlobStableIdentity(t, initial, changed)
	assertBlobContent(t, changed, 43, "Application/Changed", "blobs/Changed ")
	assertContentUpdateStats(t, hook, 518, 6)

	newerAccess := lastAccess.Add(time.Minute)
	upsertIdempotencyBlob(t, store, BlobRecord{
		Digest: digest, Size: 43, MediaType: "Application/Changed", ObjectKey: "blobs/Changed ", LastAccessAt: newerAccess,
	})
	staleAccess := upsertIdempotencyBlob(t, store, BlobRecord{
		Digest: digest, Size: 43, MediaType: "Application/Changed", ObjectKey: "blobs/Changed ", LastAccessAt: lastAccess,
	})
	assertBlobStableIdentity(t, initial, staleAccess)
	assertContentUpdateStats(t, hook, 520, 6)
	if !staleAccess.LastAccessAt.Equal(newerAccess) {
		t.Fatalf("last access after stale upsert = %s, want max %s", staleAccess.LastAccessAt, newerAccess)
	}
}

func openBlobWriteIdempotencyStore(t *testing.T, hook *blobWriteIdempotencyHook) *SQLStore {
	t.Helper()
	driver := strings.ToLower(strings.TrimSpace(os.Getenv("REGIMUX_META_CONCURRENCY_DRIVER")))
	dsn := os.Getenv("REGIMUX_META_CONCURRENCY_DSN")
	if (driver == "") != (dsn == "") {
		t.Fatal("set both REGIMUX_META_CONCURRENCY_DRIVER and REGIMUX_META_CONCURRENCY_DSN")
	}
	options := DBOptions{Hooks: []dbx.Hook{hook.dbHook()}}
	var store *SQLStore
	var err error
	if driver != "" && dsn != "" {
		options.Driver = driver
		options.DSN = dsn
		store, err = OpenDBWithOptions(t.Context(), options)
	} else {
		options.Path = filepath.Join(t.TempDir(), "blob-write-idempotency.db")
		store, err = OpenSQLiteWithOptions(t.Context(), options)
	}
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Logf("metadata backend=%s", store.driver)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close metadata store: %v", err)
		}
	})
	return store
}

func upsertIdempotencyBlob(t *testing.T, store *SQLStore, record BlobRecord) *BlobRecord {
	t.Helper()
	result, err := store.UpsertBlob(t.Context(), record)
	if err != nil {
		t.Fatalf("upsert blob: %v", err)
	}
	return result
}

func readIdempotencyBlob(t *testing.T, store *SQLStore, digest string) *BlobRecord {
	t.Helper()
	record, found, err := store.Blob(t.Context(), BlobKey{Digest: digest})
	if err != nil || !found {
		t.Fatalf("read blob: found=%v error=%v", found, err)
	}
	return record
}

func assertBlobStableIdentity(t *testing.T, before, after *BlobRecord) {
	t.Helper()
	if after.ID != before.ID {
		t.Fatalf("blob ID changed from %d to %d", before.ID, after.ID)
	}
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("blob created_at changed from %s to %s", before.CreatedAt, after.CreatedAt)
	}
}

func assertBlobUpdatedAtStable(t *testing.T, before, after *BlobRecord) {
	t.Helper()
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("blob updated_at changed from %s to %s", before.UpdatedAt, after.UpdatedAt)
	}
}

func assertBlobContent(t *testing.T, record *BlobRecord, size int64, mediaType, objectKey string) {
	t.Helper()
	if record.Size != size || record.MediaType != mediaType || record.ObjectKey != objectKey {
		t.Fatalf("blob content = size:%d media:%q object:%q, want %d/%q/%q",
			record.Size, record.MediaType, record.ObjectKey, size, mediaType, objectKey)
	}
}

func assertContentUpdateStats(t *testing.T, hook *blobWriteIdempotencyHook, attempts, affected int64) {
	t.Helper()
	gotAttempts, gotAffected, missing := hook.contentUpdateStats()
	if gotAttempts != attempts {
		t.Fatalf("blob content UPDATE attempts = %d, want %d", gotAttempts, attempts)
	}
	if missing != 0 {
		t.Fatalf("blob content UPDATE events without RowsAffected = %d, want 0", missing)
	}
	if gotAffected != affected {
		t.Fatalf("blob content UPDATE affected rows = %d, want %d", gotAffected, affected)
	}
}

type blobWriteIdempotencyHook struct {
	armed    atomic.Bool
	mu       sync.Mutex
	attempts int64
	affected int64
	missing  int64
}

func (h *blobWriteIdempotencyHook) dbHook() dbx.Hook {
	return dbx.HookFuncs{AfterFunc: h.after}
}

func (h *blobWriteIdempotencyHook) after(_ context.Context, event *dbx.HookEvent) {
	if !h.armed.Load() || event.Err != nil || event.Operation != dbx.OperationExec || !isBlobContentUpdate(event.SQL) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attempts++
	if !event.HasRowsAffected {
		h.missing++
		return
	}
	h.affected += event.RowsAffected
}

func (h *blobWriteIdempotencyHook) contentUpdateStats() (int64, int64, int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attempts, h.affected, h.missing
}

func isBlobContentUpdate(statement string) bool {
	sql := strings.ToLower(strings.TrimSpace(statement))
	return strings.HasPrefix(sql, "update") && strings.Contains(sql, "meta_blobs") &&
		strings.Contains(sql, "size") && strings.Contains(sql, "media_type") && strings.Contains(sql, "object_key")
}
