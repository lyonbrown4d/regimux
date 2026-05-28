package meta_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const secondTestDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestSQLiteStoreManifestCRUD(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStore(ctx, t)
	expires := time.Now().UTC().Add(time.Hour)

	manifest := upsertManifest(ctx, t, store, expires)
	if manifest.ID == 0 || manifest.Key != "hub/library/nginx@"+testDigest || manifest.CreatedAt.IsZero() || manifest.UpdatedAt.IsZero() {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	assertManifestIDStableAfterUpdate(ctx, t, store, manifest)

	got, ok := getManifest(ctx, t, store)
	if !ok || got.MediaType != manifest.MediaType || got.Headers["Docker-Content-Digest"][0] != testDigest {
		t.Fatalf("unexpected manifest lookup: ok=%v record=%#v", ok, got)
	}
	if !got.Expired(expires.Add(time.Nanosecond)) {
		t.Fatal("expected manifest to be expired after expires_at")
	}
}

func TestSQLiteStoreTagCRUD(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStore(ctx, t)
	expires := time.Now().UTC().Add(time.Hour)

	tag, err := store.UpsertTag(ctx, meta.TagRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Reference:  "latest",
		Digest:     testDigest,
		ExpiresAt:  expires,
	})
	requireNoError(t, "upsert tag", err)
	if tag.ID == 0 || tag.Key != "hub/library/nginx:latest" {
		t.Fatalf("unexpected tag: %#v", tag)
	}

	got, ok, err := store.Tag(ctx, meta.TagKey{Alias: "hub", Repository: "library/nginx", Reference: "latest"})
	requireNoError(t, "get tag", err)
	if !ok || got.Digest != testDigest {
		t.Fatalf("unexpected tag lookup: ok=%v record=%#v", ok, got)
	}

	err = store.DeleteTag(ctx, meta.TagKey{Alias: "hub", Repository: "library/nginx", Reference: "latest"})
	requireNoError(t, "delete tag", err)
	_, ok, err = store.Tag(ctx, meta.TagKey{Alias: "hub", Repository: "library/nginx", Reference: "latest"})
	requireNoError(t, "get deleted tag", err)
	if ok {
		t.Fatal("expected tag to be deleted")
	}
}

func TestSQLiteStoreBlobCRUD(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStore(ctx, t)

	blob, err := store.UpsertBlob(ctx, meta.BlobRecord{
		Digest:    testDigest,
		Size:      2048,
		MediaType: "application/octet-stream",
		ObjectKey: testDigest,
	})
	requireNoError(t, "upsert blob", err)
	if blob.ID == 0 {
		t.Fatalf("unexpected blob id: %#v", blob)
	}

	got, ok, err := store.Blob(ctx, meta.BlobKey{Digest: testDigest})
	requireNoError(t, "get blob", err)
	if !ok || got.Size != blob.Size || got.Digest != testDigest {
		t.Fatalf("unexpected blob lookup: ok=%v record=%#v", ok, got)
	}
}

func TestSQLiteStoreRepoBlobCRUD(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStore(ctx, t)

	repoBlob, err := store.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Digest:     testDigest,
	})
	requireNoError(t, "upsert repo blob", err)
	if repoBlob.ID == 0 || repoBlob.Key != "hub/library/nginx@"+testDigest || repoBlob.LastVerifiedAt.IsZero() {
		t.Fatalf("unexpected repo blob: %#v", repoBlob)
	}

	got, ok, err := store.RepoBlob(ctx, meta.RepoBlobKey{Alias: "hub", Repository: "library/nginx", Digest: testDigest})
	requireNoError(t, "get repo blob", err)
	if !ok || got.Digest != testDigest {
		t.Fatalf("unexpected repo blob lookup: ok=%v record=%#v", ok, got)
	}
}

func TestSQLiteStorePullRecords(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStore(ctx, t)
	first := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	key := meta.PullKey{Alias: "hub", Repository: "library/node", Reference: "20"}

	pull, err := store.RecordPull(ctx, key, first)
	requireNoError(t, "record pull", err)
	if pull.ID == 0 {
		t.Fatalf("unexpected pull id: %#v", pull)
	}
	firstID := pull.ID
	assertPullRecord(t, pull, 1, first, time.Time{})
	pull, err = store.RecordPull(ctx, key, second)
	requireNoError(t, "record second pull", err)
	if pull.ID != firstID {
		t.Fatalf("unexpected pull id after second pull: first=%d next=%d", firstID, pull.ID)
	}
	assertPullRecord(t, pull, 2, second, time.Time{})
	pull, err = store.RecordUpstreamPull(ctx, key, second)
	requireNoError(t, "record upstream pull", err)
	assertPullRecord(t, pull, 2, second, second)

	got, ok, err := store.Pull(ctx, key)
	requireNoError(t, "get pull", err)
	assertPullLookup(t, got, ok, second)
}

func TestSQLiteStoreListsRecords(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStore(ctx, t)
	expires := time.Now().UTC().Add(time.Hour)

	seedListRecords(ctx, t, store, expires)

	manifests, err := store.ListManifests(ctx)
	requireNoError(t, "list manifests", err)
	assertManifestList(t, manifests)
	tags, err := store.ListTags(ctx)
	requireNoError(t, "list tags", err)
	assertTagList(t, tags)
	pulls, err := store.ListPulls(ctx)
	requireNoError(t, "list pulls", err)
	assertPullList(t, pulls)
	blobs, err := store.ListBlobs(ctx)
	requireNoError(t, "list blobs", err)
	assertBlobList(t, blobs)
	repoBlobs, err := store.ListRepoBlobs(ctx)
	requireNoError(t, "list repo blobs", err)
	assertRepoBlobList(t, repoBlobs)
}

func TestSQLiteStorePersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "regimux.db")
	store := openSQLiteStore(ctx, t, path)

	_, err := store.UpsertBlob(ctx, meta.BlobRecord{Digest: testDigest, Size: 42})
	requireNoError(t, "upsert blob", err)
	closeSQLiteStore(t, store)

	reopened := openSQLiteStore(ctx, t, path)
	t.Cleanup(func() { closeSQLiteStore(t, reopened) })
	got, ok, err := reopened.Blob(ctx, meta.BlobKey{Digest: testDigest})
	requireNoError(t, "get blob", err)
	if !ok || got.Size != 42 {
		t.Fatalf("unexpected reopened blob: ok=%v record=%#v", ok, got)
	}
}

func TestSQLiteStoreValidatesRecords(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStore(ctx, t)

	_, err := store.UpsertBlob(ctx, meta.BlobRecord{Digest: "not-a-digest"})
	if err == nil {
		t.Fatal("expected invalid digest error")
	}
	_, err = store.UpsertManifest(ctx, meta.ManifestRecord{
		Alias:      "hub",
		Repository: "repo",
		Digest:     testDigest,
		Size:       -1,
	})
	if err == nil {
		t.Fatal("expected negative size error")
	}
}

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
