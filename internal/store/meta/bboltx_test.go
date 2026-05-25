package meta

import (
	"context"
	"testing"
	"time"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestBboltStoreManifestTagBlobCRUD(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/regimux.db"
	store, err := OpenBboltWithOptions(ctx, BboltOptions{Path: path})
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	defer store.Close()

	expires := time.Now().UTC().Add(time.Hour)
	manifest, err := store.UpsertManifest(ctx, ManifestRecord{
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
	if err != nil {
		t.Fatalf("upsert manifest: %v", err)
	}
	if manifest.Key != "hub/library/nginx@"+testDigest || manifest.CreatedAt.IsZero() || manifest.UpdatedAt.IsZero() {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}

	gotManifest, ok, err := store.Manifest(ctx, ManifestKey{Alias: "hub", Repository: "library/nginx", Digest: testDigest})
	if err != nil {
		t.Fatalf("get manifest: %v", err)
	}
	if !ok || gotManifest.MediaType != manifest.MediaType || gotManifest.Headers["Docker-Content-Digest"][0] != testDigest {
		t.Fatalf("unexpected manifest lookup: ok=%v record=%#v", ok, gotManifest)
	}
	if !gotManifest.Expired(expires.Add(time.Nanosecond)) {
		t.Fatal("expected manifest to be expired after expires_at")
	}

	tag, err := store.UpsertTag(ctx, TagRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Reference:  "latest",
		Digest:     testDigest,
		ExpiresAt:  expires,
	})
	if err != nil {
		t.Fatalf("upsert tag: %v", err)
	}
	if tag.Key != "hub/library/nginx:latest" {
		t.Fatalf("unexpected tag key: %s", tag.Key)
	}
	gotTag, ok, err := store.Tag(ctx, TagKey{Alias: "hub", Repository: "library/nginx", Reference: "latest"})
	if err != nil {
		t.Fatalf("get tag: %v", err)
	}
	if !ok || gotTag.Digest != testDigest {
		t.Fatalf("unexpected tag lookup: ok=%v record=%#v", ok, gotTag)
	}

	blob, err := store.UpsertBlob(ctx, BlobRecord{
		Digest:    testDigest,
		Size:      2048,
		MediaType: "application/octet-stream",
		ObjectKey: testDigest,
	})
	if err != nil {
		t.Fatalf("upsert blob: %v", err)
	}
	gotBlob, ok, err := store.Blob(ctx, BlobKey{Digest: testDigest})
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	if !ok || gotBlob.Size != blob.Size || gotBlob.Digest != testDigest {
		t.Fatalf("unexpected blob lookup: ok=%v record=%#v", ok, gotBlob)
	}

	repoBlob, err := store.UpsertRepoBlob(ctx, RepoBlobRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Digest:     testDigest,
	})
	if err != nil {
		t.Fatalf("upsert repo blob: %v", err)
	}
	if repoBlob.Key != "hub/library/nginx@"+testDigest || repoBlob.LastVerifiedAt.IsZero() {
		t.Fatalf("unexpected repo blob: %#v", repoBlob)
	}
	gotRepoBlob, ok, err := store.RepoBlob(ctx, RepoBlobKey{Alias: "hub", Repository: "library/nginx", Digest: testDigest})
	if err != nil {
		t.Fatalf("get repo blob: %v", err)
	}
	if !ok || gotRepoBlob.Digest != testDigest {
		t.Fatalf("unexpected repo blob lookup: ok=%v record=%#v", ok, gotRepoBlob)
	}

	if err := store.DeleteTag(ctx, TagKey{Alias: "hub", Repository: "library/nginx", Reference: "latest"}); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	_, ok, err = store.Tag(ctx, TagKey{Alias: "hub", Repository: "library/nginx", Reference: "latest"})
	if err != nil {
		t.Fatalf("get deleted tag: %v", err)
	}
	if ok {
		t.Fatal("expected tag to be deleted")
	}
}

func TestBboltStorePersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/regimux.db"
	store, err := OpenBboltWithOptions(ctx, BboltOptions{Path: path})
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	if _, err := store.UpsertBlob(ctx, BlobRecord{Digest: testDigest, Size: 42}); err != nil {
		t.Fatalf("upsert blob: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := OpenBboltWithOptions(ctx, BboltOptions{Path: path})
	if err != nil {
		t.Fatalf("reopen bbolt: %v", err)
	}
	defer reopened.Close()
	got, ok, err := reopened.Blob(ctx, BlobKey{Digest: testDigest})
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	if !ok || got.Size != 42 {
		t.Fatalf("unexpected reopened blob: ok=%v record=%#v", ok, got)
	}
}

func TestBboltStoreValidatesRecords(t *testing.T) {
	ctx := context.Background()
	store, err := OpenBboltWithOptions(ctx, BboltOptions{Path: t.TempDir() + "/regimux.db"})
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	defer store.Close()

	if _, err := store.UpsertBlob(ctx, BlobRecord{Digest: "not-a-digest"}); err == nil {
		t.Fatal("expected invalid digest error")
	}
	if _, err := store.UpsertManifest(ctx, ManifestRecord{Alias: "hub", Repository: "repo", Digest: testDigest, Size: -1}); err == nil {
		t.Fatal("expected negative size error")
	}
}
