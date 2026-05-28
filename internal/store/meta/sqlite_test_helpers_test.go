package meta_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func newSQLiteStore(ctx context.Context, t *testing.T) *meta.SQLiteStore {
	t.Helper()
	store := openSQLiteStore(ctx, t, filepath.Join(t.TempDir(), "regimux.db"))
	t.Cleanup(func() { closeSQLiteStore(t, store) })
	return store
}

func openSQLiteStore(ctx context.Context, t *testing.T, path string) *meta.SQLiteStore {
	t.Helper()
	store, err := meta.OpenSQLiteWithOptions(ctx, meta.SQLiteOptions{Path: path})
	requireNoError(t, "open sqlite", err)
	return store
}

func closeSQLiteStore(t *testing.T, store *meta.SQLiteStore) {
	t.Helper()
	err := store.Close()
	requireNoError(t, "close sqlite", err)
}

type recordingDBHook struct {
	afterOps  []dbx.Operation
	durations []time.Duration
}

func (h *recordingDBHook) Before(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	_ = event
	return ctx, nil
}

func (h *recordingDBHook) After(_ context.Context, event *dbx.HookEvent) {
	if event == nil {
		return
	}
	h.afterOps = append(h.afterOps, event.Operation)
	h.durations = append(h.durations, event.Duration)
}

func (h *recordingDBHook) saw(operation dbx.Operation) bool {
	return slices.Contains(h.afterOps, operation)
}

func (h *recordingDBHook) hasNegativeDuration() bool {
	for _, duration := range h.durations {
		if duration < 0 {
			return true
		}
	}
	return false
}

func upsertManifest(
	ctx context.Context,
	t *testing.T,
	store *meta.SQLiteStore,
	expires time.Time,
) *meta.ManifestRecord {
	t.Helper()
	manifest, err := store.UpsertManifest(ctx, meta.ManifestRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Digest:     testDigest,
		MediaType:  "application/vnd.oci.image.manifest.v1+json",
		Size:       128,
		ObjectKey:  testDigest,
		Headers: map[string][]string{
			"Docker-Content-Digest": {testDigest},
		},
		ExpiresAt: expires,
	})
	requireNoError(t, "upsert manifest", err)
	return manifest
}

func getManifest(ctx context.Context, t *testing.T, store *meta.SQLiteStore) (*meta.ManifestRecord, bool) {
	t.Helper()
	got, ok, err := store.Manifest(ctx, meta.ManifestKey{Alias: "hub", Repository: "library/nginx", Digest: testDigest})
	requireNoError(t, "get manifest", err)
	return got, ok
}

func assertManifestIDStableAfterUpdate(ctx context.Context, t *testing.T, store *meta.SQLiteStore, manifest *meta.ManifestRecord) {
	t.Helper()
	updatedManifest := *manifest
	updatedManifest.Size = 256
	updated, err := store.UpsertManifest(ctx, updatedManifest)
	requireNoError(t, "upsert manifest again", err)
	if updated.ID != manifest.ID || updated.Size != 256 {
		t.Fatalf("unexpected manifest id after update: before=%#v after=%#v", manifest, updated)
	}
}

func requireNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}
