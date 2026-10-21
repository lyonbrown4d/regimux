package meta

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadCleanupSnapshotPagesAllRows(t *testing.T) {
	ctx := context.Background()
	store := openCleanupSnapshotStore(t)
	insertCleanupTestBlobs(ctx, t, store, 257)

	ctx = WithCleanupSnapshotLimits(ctx, CleanupSnapshotLimits{MaxRecords: 300, MaxBytes: 1 << 20})
	snapshot, err := ReadCleanupSnapshot(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(snapshot.Blobs); got != 257 {
		t.Fatalf("cleanup snapshot blobs = %d, want all 257 rows across pages", got)
	}
	if snapshot.Blobs[0].ID >= snapshot.Blobs[len(snapshot.Blobs)-1].ID {
		t.Fatalf("cleanup snapshot is not ordered by primary key: first=%d last=%d", snapshot.Blobs[0].ID, snapshot.Blobs[len(snapshot.Blobs)-1].ID)
	}
}

func TestReadCleanupSnapshotSharesRecordBudgetAcrossTables(t *testing.T) {
	ctx := context.Background()
	store := openCleanupSnapshotStore(t)
	insertCleanupTestBlobs(ctx, t, store, 2)

	ctx = WithCleanupSnapshotLimits(ctx, CleanupSnapshotLimits{MaxRecords: 1, MaxBytes: 1 << 20})
	_, err := ReadCleanupSnapshot(ctx, store)
	if !errors.Is(err, ErrCleanupSnapshotLimit) {
		t.Fatalf("snapshot error = %v, want record budget error", err)
	}
}

func TestReadCleanupSnapshotChecksRawVariableRowBytesBeforeMapping(t *testing.T) {
	ctx := context.Background()
	store := openCleanupSnapshotStore(t)
	_, err := store.UpsertManifest(ctx, ManifestRecord{
		Alias: "hub", Repository: "library/demo", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		MediaType: "application/vnd.oci.image.manifest.v1+json", Size: 1,
		Headers:   map[string][]string{"Content-Type": {strings.Repeat("x", 1024)}},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx = WithCleanupSnapshotLimits(ctx, CleanupSnapshotLimits{MaxRecords: 10, MaxBytes: 512})
	_, err = ReadCleanupSnapshot(ctx, store)
	if !errors.Is(err, ErrCleanupSnapshotLimit) {
		t.Fatalf("snapshot error = %v, want raw row byte budget error", err)
	}
}

func TestReadCleanupSnapshotHonorsCancellation(t *testing.T) {
	store := openCleanupSnapshotStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ReadCleanupSnapshot(ctx, store)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshot error = %v, want context cancellation", err)
	}
}

func openCleanupSnapshotStore(t *testing.T) *SQLStore {
	t.Helper()
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "cleanup-snapshot.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	return store
}

func insertCleanupTestBlobs(ctx context.Context, t *testing.T, store *SQLStore, count int) {
	t.Helper()
	tx, err := store.db.SQLDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackCleanupTestTransaction(t, tx)
	statement, err := tx.PrepareContext(ctx, `INSERT INTO "meta_blobs" ("id", "digest", "size", "media_type", "object_key", "created_at", "updated_at", "last_access_at") VALUES (?, ?, 1, ?, ?, 1, 1, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := statement.Close(); closeErr != nil {
			t.Errorf("close cleanup test statement: %v", closeErr)
		}
	}()
	for id := 1; id <= count; id++ {
		digest := fmt.Sprintf("sha256:%064x", id)
		if _, err := statement.ExecContext(ctx, id, digest, "application/octet-stream", digest); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func rollbackCleanupTestTransaction(t *testing.T, tx *sql.Tx) {
	t.Helper()
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("rollback cleanup test transaction: %v", err)
	}
}
