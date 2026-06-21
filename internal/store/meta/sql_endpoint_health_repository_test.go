package meta_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestSQLStoreEndpointHealthPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "regimux.db")
	store := openSQLStore(ctx, t, path)

	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	record := insertEndpointHealth(ctx, t, store, now)
	createdAt := record.CreatedAt
	updateEndpointHealth(ctx, t, store, record, now)
	closeSQLStore(t, store)

	reopened := openSQLStore(ctx, t, path)
	t.Cleanup(func() { closeSQLStore(t, reopened) })
	assertEndpointHealthLookup(ctx, t, reopened, createdAt)
	assertEndpointHealthList(ctx, t, reopened)
}

func TestSQLStorePutEndpointHealthSnapshotPreservesCreatedAt(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)

	err := store.PutEndpointHealthSnapshot(ctx, meta.EndpointHealthRecord{
		Alias:          "hub",
		Registry:       "https://mirror.example/",
		Repository:     "library/nginx",
		LatencyEWMA:    120 * time.Millisecond,
		LatencySamples: 3,
		SuccessCount:   8,
		LastProbeAt:    now,
	})
	requireNoError(t, "put endpoint health snapshot", err)
	first, ok, err := store.EndpointHealth(ctx, meta.EndpointHealthKey{
		Alias:      "hub",
		Registry:   "https://mirror.example",
		Repository: "library/nginx",
	})
	requireNoError(t, "get endpoint health", err)
	if !ok {
		t.Fatal("expected endpoint health snapshot")
	}

	err = store.PutEndpointHealthSnapshot(ctx, meta.EndpointHealthRecord{
		Alias:          "hub",
		Registry:       "https://mirror.example",
		Repository:     "library/nginx",
		LatencyEWMA:    90 * time.Millisecond,
		LatencySamples: 4,
		SuccessCount:   9,
		LastProbeAt:    now.Add(time.Second),
	})
	requireNoError(t, "update endpoint health snapshot", err)
	second, ok, err := store.EndpointHealth(ctx, meta.EndpointHealthKey{
		Alias:      "hub",
		Registry:   "https://mirror.example",
		Repository: "library/nginx",
	})
	requireNoError(t, "get endpoint health after update", err)
	if !ok || second.ID != first.ID || !second.CreatedAt.Equal(first.CreatedAt) || second.SuccessCount != 9 || second.LatencyEWMA != 90*time.Millisecond {
		t.Fatalf("unexpected endpoint health snapshot update: before=%#v after=%#v", first, second)
	}
}

func insertEndpointHealth(ctx context.Context, t *testing.T, store *meta.SQLStore, now time.Time) *meta.EndpointHealthRecord {
	t.Helper()
	record, err := store.UpsertEndpointHealth(ctx, meta.EndpointHealthRecord{
		Alias:                "hub",
		Registry:             "https://mirror.example/",
		Repository:           "library/nginx",
		LatencyEWMA:          120 * time.Millisecond,
		LatencySamples:       3,
		ConsecutiveFailures:  2,
		SuccessCount:         8,
		FailureCount:         2,
		ContentMismatchCount: 1,
		CooldownUntil:        now.Add(time.Minute),
		DegradedUntil:        now.Add(2 * time.Minute),
		LastSuccessAt:        now,
		LastFailureAt:        now.Add(time.Second),
		LastProbeAt:          now.Add(2 * time.Second),
	})
	requireNoError(t, "upsert endpoint health", err)
	if record.ID == 0 || record.Key == "" || record.Registry != "https://mirror.example" {
		t.Fatalf("unexpected endpoint health record: %#v", record)
	}
	return record
}

func updateEndpointHealth(ctx context.Context, t *testing.T, store *meta.SQLStore, previous *meta.EndpointHealthRecord, now time.Time) {
	t.Helper()
	updated, err := store.UpsertEndpointHealth(ctx, meta.EndpointHealthRecord{
		Alias:          "hub",
		Registry:       "https://mirror.example",
		Repository:     "library/nginx",
		LatencyEWMA:    90 * time.Millisecond,
		LatencySamples: 4,
		SuccessCount:   9,
		LastProbeAt:    now.Add(3 * time.Second),
	})
	requireNoError(t, "update endpoint health", err)
	if updated.ID != previous.ID || !updated.CreatedAt.Equal(previous.CreatedAt) {
		t.Fatalf("endpoint health identity changed after update: before=%#v after=%#v", previous, updated)
	}
	if updated.SuccessCount != 9 || updated.LatencyEWMA != 90*time.Millisecond {
		t.Fatalf("endpoint health update did not persist in returned record: %#v", updated)
	}
}

func assertEndpointHealthLookup(ctx context.Context, t *testing.T, store *meta.SQLStore, createdAt time.Time) {
	t.Helper()
	got, ok, err := store.EndpointHealth(ctx, meta.EndpointHealthKey{
		Alias:      "hub",
		Registry:   "https://mirror.example",
		Repository: "library/nginx",
	})
	requireNoError(t, "get endpoint health", err)
	if !ok || got.SuccessCount != 9 || got.LatencyEWMA != 90*time.Millisecond || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected endpoint health lookup: ok=%v record=%#v", ok, got)
	}
}

func assertEndpointHealthList(ctx context.Context, t *testing.T, store *meta.SQLStore) {
	t.Helper()
	list, err := store.ListEndpointHealth(ctx, meta.EndpointHealthListAlias("hub"))
	requireNoError(t, "list endpoint health", err)
	if len(list) != 1 || list[0].Repository != "library/nginx" {
		t.Fatalf("unexpected endpoint health list: %#v", list)
	}
}
