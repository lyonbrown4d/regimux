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

func TestBboltStoreManifestCRUD(t *testing.T) {
	ctx := context.Background()
	store := newBboltStore(ctx, t)
	expires := time.Now().UTC().Add(time.Hour)

	manifest := upsertManifest(ctx, t, store, expires)
	if manifest.Key != "hub/library/nginx@"+testDigest || manifest.CreatedAt.IsZero() || manifest.UpdatedAt.IsZero() {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}

	got, ok := getManifest(ctx, t, store)
	if !ok || got.MediaType != manifest.MediaType || got.Headers["Docker-Content-Digest"][0] != testDigest {
		t.Fatalf("unexpected manifest lookup: ok=%v record=%#v", ok, got)
	}
	if !got.Expired(expires.Add(time.Nanosecond)) {
		t.Fatal("expected manifest to be expired after expires_at")
	}
}

func TestBboltStoreTagCRUD(t *testing.T) {
	ctx := context.Background()
	store := newBboltStore(ctx, t)
	expires := time.Now().UTC().Add(time.Hour)

	tag, err := store.UpsertTag(ctx, meta.TagRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Reference:  "latest",
		Digest:     testDigest,
		ExpiresAt:  expires,
	})
	requireNoError(t, "upsert tag", err)
	if tag.Key != "hub/library/nginx:latest" {
		t.Fatalf("unexpected tag key: %s", tag.Key)
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

func TestBboltStoreBlobCRUD(t *testing.T) {
	ctx := context.Background()
	store := newBboltStore(ctx, t)

	blob, err := store.UpsertBlob(ctx, meta.BlobRecord{
		Digest:    testDigest,
		Size:      2048,
		MediaType: "application/octet-stream",
		ObjectKey: testDigest,
	})
	requireNoError(t, "upsert blob", err)

	got, ok, err := store.Blob(ctx, meta.BlobKey{Digest: testDigest})
	requireNoError(t, "get blob", err)
	if !ok || got.Size != blob.Size || got.Digest != testDigest {
		t.Fatalf("unexpected blob lookup: ok=%v record=%#v", ok, got)
	}
}

func TestBboltStoreRepoBlobCRUD(t *testing.T) {
	ctx := context.Background()
	store := newBboltStore(ctx, t)

	repoBlob, err := store.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Digest:     testDigest,
	})
	requireNoError(t, "upsert repo blob", err)
	if repoBlob.Key != "hub/library/nginx@"+testDigest || repoBlob.LastVerifiedAt.IsZero() {
		t.Fatalf("unexpected repo blob: %#v", repoBlob)
	}

	got, ok, err := store.RepoBlob(ctx, meta.RepoBlobKey{Alias: "hub", Repository: "library/nginx", Digest: testDigest})
	requireNoError(t, "get repo blob", err)
	if !ok || got.Digest != testDigest {
		t.Fatalf("unexpected repo blob lookup: ok=%v record=%#v", ok, got)
	}
}

func TestBboltStorePullRecords(t *testing.T) {
	ctx := context.Background()
	store := newBboltStore(ctx, t)
	first := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	key := meta.PullKey{Alias: "hub", Repository: "library/node", Reference: "20"}

	pull, err := store.RecordPull(ctx, key, first)
	requireNoError(t, "record pull", err)
	assertPullRecord(t, pull, 1, first, time.Time{})
	pull, err = store.RecordPull(ctx, key, second)
	requireNoError(t, "record second pull", err)
	assertPullRecord(t, pull, 2, second, time.Time{})
	pull, err = store.RecordUpstreamPull(ctx, key, second)
	requireNoError(t, "record upstream pull", err)
	assertPullRecord(t, pull, 2, second, second)

	got, ok, err := store.Pull(ctx, key)
	requireNoError(t, "get pull", err)
	assertPullLookup(t, got, ok, second)
}

func TestBboltStoreListsRecords(t *testing.T) {
	ctx := context.Background()
	store := newBboltStore(ctx, t)
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

func TestBboltStorePersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "regimux.db")
	store := openBboltStore(ctx, t, path)

	_, err := store.UpsertBlob(ctx, meta.BlobRecord{Digest: testDigest, Size: 42})
	requireNoError(t, "upsert blob", err)
	closeBboltStore(t, store)

	reopened := openBboltStore(ctx, t, path)
	t.Cleanup(func() { closeBboltStore(t, reopened) })
	got, ok, err := reopened.Blob(ctx, meta.BlobKey{Digest: testDigest})
	requireNoError(t, "get blob", err)
	if !ok || got.Size != 42 {
		t.Fatalf("unexpected reopened blob: ok=%v record=%#v", ok, got)
	}
}

func TestBboltStoreValidatesRecords(t *testing.T) {
	ctx := context.Background()
	store := newBboltStore(ctx, t)

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

func newBboltStore(ctx context.Context, t *testing.T) *meta.BboltStore {
	t.Helper()
	store := openBboltStore(ctx, t, filepath.Join(t.TempDir(), "regimux.db"))
	t.Cleanup(func() { closeBboltStore(t, store) })
	return store
}

func openBboltStore(ctx context.Context, t *testing.T, path string) *meta.BboltStore {
	t.Helper()
	store, err := meta.OpenBboltWithOptions(ctx, meta.BboltOptions{Path: path})
	requireNoError(t, "open bbolt", err)
	return store
}

func closeBboltStore(t *testing.T, store *meta.BboltStore) {
	t.Helper()
	err := store.Close()
	requireNoError(t, "close bbolt", err)
}

func upsertManifest(
	ctx context.Context,
	t *testing.T,
	store *meta.BboltStore,
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

func getManifest(ctx context.Context, t *testing.T, store *meta.BboltStore) (*meta.ManifestRecord, bool) {
	t.Helper()
	got, ok, err := store.Manifest(ctx, meta.ManifestKey{Alias: "hub", Repository: "library/nginx", Digest: testDigest})
	requireNoError(t, "get manifest", err)
	return got, ok
}

func requireNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}
