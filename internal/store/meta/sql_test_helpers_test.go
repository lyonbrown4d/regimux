package meta_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func newSQLStore(ctx context.Context, t *testing.T) *meta.SQLStore {
	t.Helper()
	store := openSQLStore(ctx, t, filepath.Join(t.TempDir(), "regimux.db"))
	t.Cleanup(func() { closeSQLStore(t, store) })
	return store
}

func openSQLStore(ctx context.Context, t *testing.T, path string) *meta.SQLStore {
	t.Helper()
	store, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: path})
	requireNoError(t, "open sqlite", err)
	return store
}

func closeSQLStore(t *testing.T, store *meta.SQLStore) {
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
	store *meta.SQLStore,
	expires time.Time,
) *meta.ManifestRecord {
	t.Helper()
	manifest, err := store.UpsertManifest(ctx, meta.ManifestRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Digest:     testDigest,
		MediaType:  distribution.MediaTypeOCIManifest,
		Size:       128,
		ObjectKey:  testDigest,
		Headers: map[string][]string{
			distribution.HeaderDockerContentDigest: {testDigest},
		},
		ExpiresAt: expires,
	})
	requireNoError(t, "upsert manifest", err)
	return manifest
}

func getManifest(ctx context.Context, t *testing.T, store *meta.SQLStore) (*meta.ManifestRecord, bool) {
	t.Helper()
	got, ok, err := store.Manifest(ctx, meta.ManifestKey{Alias: "hub", Repository: "library/nginx", Digest: testDigest})
	requireNoError(t, "get manifest", err)
	return got, ok
}

func assertManifestIDStableAfterUpdate(ctx context.Context, t *testing.T, store *meta.SQLStore, manifest *meta.ManifestRecord) {
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
