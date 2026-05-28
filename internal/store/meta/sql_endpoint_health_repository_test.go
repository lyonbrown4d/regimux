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
	closeSQLStore(t, store)

	reopened := openSQLStore(ctx, t, path)
	t.Cleanup(func() { closeSQLStore(t, reopened) })
	got, ok, err := reopened.EndpointHealth(ctx, meta.EndpointHealthKey{
		Alias:      "hub",
		Registry:   "https://mirror.example",
		Repository: "library/nginx",
	})
	requireNoError(t, "get endpoint health", err)
	if !ok || got.SuccessCount != 8 || got.FailureCount != 2 || got.LatencyEWMA != 120*time.Millisecond {
		t.Fatalf("unexpected endpoint health lookup: ok=%v record=%#v", ok, got)
	}

	list, err := reopened.ListEndpointHealth(ctx, meta.EndpointHealthListAlias("hub"))
	requireNoError(t, "list endpoint health", err)
	if len(list) != 1 || list[0].Repository != "library/nginx" {
		t.Fatalf("unexpected endpoint health list: %#v", list)
	}
}
