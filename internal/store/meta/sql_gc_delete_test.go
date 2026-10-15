package meta_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/stretchr/testify/require"
)

func TestConditionalGCDeletesOnlyMatchingMetadataRevisions(t *testing.T) {
	store := openConditionalGCStore(t)
	verifyConditionalBlobDeletion(t, store)
	verifyConditionalTagDeletion(t, store)
}

func openConditionalGCStore(t *testing.T) *meta.SQLStore {
	t.Helper()
	store, err := meta.OpenSQLite(filepath.Join(t.TempDir(), "meta.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	return store
}

func verifyConditionalBlobDeletion(t *testing.T, store *meta.SQLStore) {
	t.Helper()
	ctx := context.Background()
	digest := "sha256:" + strings.Repeat("a", 64)
	oldBlob, err := store.UpsertBlob(ctx, meta.BlobRecord{
		Digest: digest, Size: 1, ObjectKey: digest, LastAccessAt: time.Now().UTC().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedBlobRecord := *oldBlob
	updatedBlobRecord.LastAccessAt = time.Now().UTC()
	updatedBlob, updateErr := store.UpsertBlob(ctx, updatedBlobRecord)
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	persistedBlob, found, readErr := store.Blob(ctx, meta.BlobKey{Digest: digest})
	require.NoError(t, readErr)
	require.True(t, found)
	require.Equal(t, persistedBlob, updatedBlob, "upsert must return the persisted revision")
	deleted, err := store.DeleteBlobIfUnchanged(ctx, *oldBlob)
	if err != nil || deleted {
		t.Fatalf("stale blob revision deleted=%v err=%v", deleted, err)
	}
	if _, ok, lookupErr := store.Blob(ctx, meta.BlobKey{Digest: oldBlob.Digest}); lookupErr != nil || !ok {
		t.Fatalf("updated blob missing: ok=%v err=%v", ok, lookupErr)
	}
	deleted, err = store.DeleteBlobIfUnchanged(ctx, *updatedBlob)
	if err != nil || !deleted {
		t.Fatalf("current blob revision deleted=%v err=%v", deleted, err)
	}
}

func verifyConditionalTagDeletion(t *testing.T, store *meta.SQLStore) {
	t.Helper()
	ctx := context.Background()
	oldDigest := "sha256:" + strings.Repeat("b", 64)
	newDigest := "sha256:" + strings.Repeat("c", 64)
	oldTag, err := store.UpsertTag(ctx, meta.TagRecord{Alias: "hub", Repository: "pkg", Reference: "latest", Digest: oldDigest})
	if err != nil {
		t.Fatal(err)
	}
	updatedTag, updateErr := store.UpsertTag(ctx, meta.TagRecord{Alias: "hub", Repository: "pkg", Reference: "latest", Digest: newDigest})
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	deleted, err := store.DeleteTagIfUnchanged(ctx, *oldTag)
	if err != nil || deleted {
		t.Fatalf("repointed tag deleted=%v err=%v", deleted, err)
	}
	current, ok, lookupErr := store.Tag(ctx, meta.TagKey{Alias: "hub", Repository: "pkg", Reference: "latest"})
	if lookupErr != nil || !ok || current.Digest != updatedTag.Digest {
		t.Fatalf("repointed tag changed: current=%#v ok=%v err=%v", current, ok, lookupErr)
	}
}
