package meta_test

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestSQLStoreRefreshIntentDeduplicatesUntilDue(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	window := 10 * time.Minute
	record := meta.RefreshIntentRecord{
		Ecosystem:  "npm",
		Kind:       "metadata",
		Alias:      "npmjs",
		Repository: "left-pad",
		Reference:  "metadata",
	}

	queued, inserted := queueRefreshIntent(ctx, t, store, record, now, window)
	if !inserted {
		t.Fatal("first refresh intent was not queued")
	}
	if !queued.DueAt.Equal(now.Add(window)) {
		t.Fatalf("dueAt = %v, want %v", queued.DueAt, now.Add(window))
	}

	deduped, inserted := queueRefreshIntent(ctx, t, store, record, now.Add(time.Minute), window)
	if inserted {
		t.Fatal("duplicate refresh intent inside window was queued")
	}
	if deduped.ID != queued.ID || deduped.Skipped != 1 {
		t.Fatalf("unexpected deduped refresh intent: %#v", deduped)
	}
	if !deduped.DueAt.Equal(queued.DueAt) {
		t.Fatalf("deduped dueAt = %v, want %v", deduped.DueAt, queued.DueAt)
	}

	if due := consumeDueRefreshIntents(ctx, t, store, now.Add(window-time.Nanosecond)); len(due) != 0 {
		t.Fatalf("consumed %d entries before due time, want 0", len(due))
	}

	due := consumeDueRefreshIntents(ctx, t, store, now.Add(window))
	if len(due) != 1 {
		t.Fatalf("consumed %d entries at due time, want 1", len(due))
	}
	if due[0].Skipped != 1 {
		t.Fatalf("consumed skipped = %d, want 1", due[0].Skipped)
	}

	queued, inserted = queueRefreshIntent(ctx, t, store, record, now.Add(window+time.Second), window)
	if !inserted {
		t.Fatal("refresh intent after consumed window was not queued")
	}
	wantDueAt := now.Add(window + time.Second + window)
	if !queued.DueAt.Equal(wantDueAt) {
		t.Fatalf("next dueAt = %v, want %v", queued.DueAt, wantDueAt)
	}
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

	if _, inserted := queueRefreshIntent(ctx, t, store, base, now, 10*time.Minute); !inserted {
		t.Fatal("first manifest refresh was not queued")
	}
	if _, inserted := queueRefreshIntent(ctx, t, store, other, now.Add(time.Second), 10*time.Minute); !inserted {
		t.Fatal("manifest refresh with different Accept was deduplicated")
	}
	if due := consumeDueRefreshIntents(ctx, t, store, now.Add(10*time.Minute+time.Second)); len(due) != 2 {
		t.Fatalf("consumed %d entries, want 2", len(due))
	}
}

func queueRefreshIntent(
	ctx context.Context,
	t *testing.T,
	store *meta.SQLStore,
	record meta.RefreshIntentRecord,
	at time.Time,
	window time.Duration,
) (*meta.RefreshIntentRecord, bool) {
	t.Helper()
	queued, inserted, err := store.QueueRefreshIntent(ctx, record, at, window)
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
) []meta.RefreshIntentRecord {
	t.Helper()
	records, err := store.ConsumeDueRefreshIntents(ctx, at, 100)
	requireNoError(t, "consume due refresh intents", err)
	return records
}
