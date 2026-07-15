package meta_test

import (
	"context"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const testRefreshWindow = 10 * time.Minute

func TestSQLStoreRefreshIntentDeduplicatesUntilDue(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	record := meta.RefreshIntentRecord{
		Ecosystem:  "npm",
		Kind:       "metadata",
		Alias:      "npmjs",
		Repository: "left-pad",
		Reference:  "metadata",
	}

	queued := assertQueuedRefreshIntent(ctx, t, store, record, now)
	assertDedupedRefreshIntent(ctx, t, store, record, queued, now.Add(time.Minute))
	assertDueRefreshIntentCount(ctx, t, store, now.Add(testRefreshWindow-time.Nanosecond), 0)

	due := assertDueRefreshIntentCount(ctx, t, store, now.Add(testRefreshWindow), 1)
	assertRefreshIntentSkipped(t, due.Values()[0], 1)

	next := assertQueuedRefreshIntent(ctx, t, store, record, now.Add(testRefreshWindow+time.Second))
	assertRefreshIntentDueAt(t, next, now.Add(testRefreshWindow+time.Second+testRefreshWindow))
}

func TestSQLStoreRefreshIntentSeparatesAccept(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	base := meta.RefreshIntentRecord{
		Ecosystem:  "container",
		Kind:       "manifest",
		Alias:      "hub",
		Repository: "library/alpine",
		Reference:  "latest",
		Accept:     "application/vnd.oci.image.index.v1+json",
	}
	other := base
	other.Accept = "application/vnd.docker.distribution.manifest.v2+json"

	if _, inserted := queueRefreshIntent(ctx, t, store, base, now); !inserted {
		t.Fatal("first manifest refresh was not queued")
	}
	if _, inserted := queueRefreshIntent(ctx, t, store, other, now.Add(time.Second)); !inserted {
		t.Fatal("manifest refresh with different Accept was deduplicated")
	}
	assertDueRefreshIntentCount(ctx, t, store, now.Add(testRefreshWindow+time.Second), 2)
}

func assertQueuedRefreshIntent(ctx context.Context, t *testing.T, store *meta.SQLStore, record meta.RefreshIntentRecord, at time.Time) *meta.RefreshIntentRecord {
	t.Helper()
	queued, inserted := queueRefreshIntent(ctx, t, store, record, at)
	if !inserted {
		t.Fatal("refresh intent was not queued")
	}
	if queued.ID == 0 || queued.CreatedAt.IsZero() {
		t.Fatalf("unexpected queued refresh intent identity: %#v", queued)
	}
	assertRefreshIntentDueAt(t, queued, at.Add(testRefreshWindow))
	return queued
}

func assertDedupedRefreshIntent(ctx context.Context, t *testing.T, store *meta.SQLStore, record meta.RefreshIntentRecord, queued *meta.RefreshIntentRecord, at time.Time) {
	t.Helper()
	deduped, inserted := queueRefreshIntent(ctx, t, store, record, at)
	if inserted {
		t.Fatal("duplicate refresh intent inside window was queued")
	}
	if deduped.ID != queued.ID || deduped.Skipped != 1 {
		t.Fatalf("unexpected deduped refresh intent: %#v", deduped)
	}
	if !deduped.CreatedAt.Equal(queued.CreatedAt) {
		t.Fatalf("deduped createdAt = %v, want %v", deduped.CreatedAt, queued.CreatedAt)
	}
	if !deduped.LastSeenAt.Equal(at) {
		t.Fatalf("deduped lastSeenAt = %v, want %v", deduped.LastSeenAt, at)
	}
	assertRefreshIntentDueAt(t, deduped, queued.DueAt)
}

func assertDueRefreshIntentCount(ctx context.Context, t *testing.T, store *meta.SQLStore, at time.Time, want int) *collectionlist.List[meta.RefreshIntentRecord] {
	t.Helper()
	due := consumeDueRefreshIntents(ctx, t, store, at)
	if due.Len() != want {
		t.Fatalf("consumed %d entries, want %d", due.Len(), want)
	}
	return due
}

func assertRefreshIntentSkipped(t *testing.T, record meta.RefreshIntentRecord, want int) {
	t.Helper()
	if record.Skipped != want {
		t.Fatalf("consumed skipped = %d, want %d", record.Skipped, want)
	}
}

func assertRefreshIntentDueAt(t *testing.T, record *meta.RefreshIntentRecord, want time.Time) {
	t.Helper()
	if !record.DueAt.Equal(want) {
		t.Fatalf("dueAt = %v, want %v", record.DueAt, want)
	}
}

func queueRefreshIntent(
	ctx context.Context,
	t *testing.T,
	store *meta.SQLStore,
	record meta.RefreshIntentRecord,
	at time.Time,
) (*meta.RefreshIntentRecord, bool) {
	t.Helper()
	queued, inserted, err := store.QueueRefreshIntent(ctx, record, at, testRefreshWindow)
	requireNoError(t, "queue refresh intent", err)
	if queued == nil {
		t.Fatal("queued refresh intent is nil")
	}
	return queued, inserted
}

func consumeDueRefreshIntents(
	ctx context.Context,
	t *testing.T,
	store *meta.SQLStore,
	at time.Time,
) *collectionlist.List[meta.RefreshIntentRecord] {
	t.Helper()
	records, err := store.ConsumeDueRefreshIntents(ctx, at, 100)
	requireNoError(t, "consume due refresh intents", err)
	return records
}
