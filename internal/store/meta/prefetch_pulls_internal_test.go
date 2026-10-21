package meta

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReadPrefetchPullsPagesAndPreservesRecentOrder(t *testing.T) {
	ctx := context.Background()
	store := openCleanupSnapshotStore(t)
	seedPrefetchPulls(ctx, t, store, 257)
	ctx = WithCleanupSnapshotLimits(ctx, CleanupSnapshotLimits{MaxRecords: 300, MaxBytes: 1 << 20})

	got, err := ReadPrefetchPulls(ctx, store, true)
	if err != nil {
		t.Fatal(err)
	}
	want, err := store.ListPulls(ctx, PullListRecentFirst())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Values(), want.Values()) {
		t.Fatalf("bounded recent pulls differ from ListPulls ordering")
	}
}

func TestReadPrefetchPullsFailsWholeReadAtRecordLimit(t *testing.T) {
	ctx := context.Background()
	store := openCleanupSnapshotStore(t)
	seedPrefetchPulls(ctx, t, store, 3)
	ctx = WithCleanupSnapshotLimits(ctx, CleanupSnapshotLimits{MaxRecords: 2, MaxBytes: 1 << 20})

	got, err := ReadPrefetchPulls(ctx, store, false)
	if got != nil {
		t.Fatalf("over-limit pull read returned partial records: %d", got.Len())
	}
	if !errors.Is(err, ErrCleanupSnapshotLimit) {
		t.Fatalf("pull read error = %v, want limit error", err)
	}

	rows, err := store.ListPulls(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rows.Len() != 3 {
		t.Fatalf("failed read changed stored pulls: got %d, want 3", rows.Len())
	}
}

func TestReadPrefetchPullsChecksVariableRowBytes(t *testing.T) {
	ctx := context.Background()
	store := openCleanupSnapshotStore(t)
	_, err := store.RecordPull(ctx, PullKey{
		Alias: "hub", Repository: "library/demo", Reference: "latest-" + strings.Repeat("x", 1024),
	}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	ctx = WithCleanupSnapshotLimits(ctx, CleanupSnapshotLimits{MaxRecords: 10, MaxBytes: 512})

	got, err := ReadPrefetchPulls(ctx, store, false)
	if got != nil {
		t.Fatalf("over-byte-budget pull read returned partial records: %d", got.Len())
	}
	if !errors.Is(err, ErrCleanupSnapshotLimit) {
		t.Fatalf("pull read error = %v, want byte limit error", err)
	}
}

func seedPrefetchPulls(ctx context.Context, t *testing.T, store *SQLStore, count int) {
	t.Helper()
	for index := range count {
		_, err := store.RecordPull(ctx, PullKey{
			Alias: "hub", Repository: fmt.Sprintf("library/repo-%03d", index), Reference: "latest",
		}, time.Unix(int64(index+1), 0).UTC())
		if err != nil {
			t.Fatalf("record pull %d: %v", index, err)
		}
	}
}
