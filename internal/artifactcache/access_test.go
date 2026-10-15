package artifactcache_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

type blockingBlobLookupStore struct{ artifactcache.MetadataStore }

func (s blockingBlobLookupStore) Blob(ctx context.Context, _ meta.BlobKey) (*meta.BlobRecord, bool, error) {
	<-ctx.Done()
	return nil, false, fmt.Errorf("block blob lookup: %w", ctx.Err())
}

func TestTouchUpdatesSharedBlobWithoutRegressingAccessTime(t *testing.T) {
	ctx := context.Background()
	store, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "metadata.db")})
	require.NoError(t, err)
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close metadata store: %v", closeErr)
		}
	})

	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err = store.UpsertBlob(ctx, meta.BlobRecord{
		Digest: digest, Size: 23, MediaType: "application/zip", ObjectKey: "shared/" + digest,
		LastAccessAt: old,
	})
	require.NoError(t, err)
	manifest := meta.ManifestRecord{Digest: digest, Size: 23, MediaType: "application/zip", ObjectKey: "shared/" + digest}
	newer := old.Add(time.Hour)
	err = artifactcache.Touch(ctx, store, manifest, newer)
	require.NoError(t, err)
	err = artifactcache.Touch(ctx, store, manifest, old)
	require.NoError(t, err)
	got, ok, err := store.Blob(ctx, meta.BlobKey{Digest: digest})
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, newer, got.LastAccessAt)
	olderConcurrent, newestConcurrent := newer.Add(time.Minute), newer.Add(2*time.Minute)
	var wait sync.WaitGroup
	errs := make(chan error, 2)
	for _, at := range []time.Time{olderConcurrent, newestConcurrent} {
		wait.Add(1)
		go func(at time.Time) {
			defer wait.Done()
			errs <- artifactcache.Touch(ctx, store, manifest, at)
		}(at)
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	got, _, err = store.Blob(ctx, meta.BlobKey{Digest: digest})
	require.NoError(t, err)
	require.Equal(t, newestConcurrent, got.LastAccessAt)
	require.Equal(t, int64(23), got.Size)
	require.Equal(t, "application/zip", got.MediaType)
	require.Equal(t, "shared/"+digest, got.ObjectKey)
}

func TestStoreGetTouchesBlobAccess(t *testing.T) {
	ctx := context.Background()
	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "metadata.db")})
	require.NoError(t, err)
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Errorf("close metadata store: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	require.NoError(t, err)
	store := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})
	key := artifactcache.Key{Alias: "maven-central", Repository: "org/example", Reference: "example-1.0.jar"}
	entry, err := store.Put(ctx, artifactcache.PutRequest{Key: key, Body: strings.NewReader("jar-content")})
	require.NoError(t, err)
	require.NoError(t, entry.Body.Close())
	old := time.Now().UTC().Add(-30 * 24 * time.Hour)
	blob, ok, err := metadata.Blob(ctx, meta.BlobKey{Digest: entry.Digest})
	require.NoError(t, err)
	require.True(t, ok)
	blob.LastAccessAt = old
	blob.ID = 0
	err = metadata.DeleteBlob(ctx, meta.BlobKey{Digest: entry.Digest})
	require.NoError(t, err)
	_, err = metadata.UpsertBlob(ctx, *blob)
	require.NoError(t, err)
	hit, ok, err := store.Get(ctx, key)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, hit.Body.Close())
	blob, _, err = metadata.Blob(ctx, meta.BlobKey{Digest: entry.Digest})
	require.NoError(t, err)
	require.True(t, blob.LastAccessAt.After(old))
}

func TestStoreGetIgnoresBoundedAccessTouchTimeout(t *testing.T) {
	ctx := context.Background()
	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "metadata.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Errorf("close metadata store: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := artifactcache.Key{Alias: "pypi", Repository: "packages", Reference: "demo.whl"}
	seed := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})
	entry, err := seed.Put(ctx, artifactcache.PutRequest{Key: key, Body: strings.NewReader("wheel-content")})
	if err != nil {
		t.Fatal(err)
	}
	err = entry.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	reader := artifactcache.New(artifactcache.Dependencies{
		Metadata: blockingBlobLookupStore{MetadataStore: metadata},
		Objects:  objects,
	})
	hit, ok, err := reader.Get(ctx, key)
	if err != nil || !ok {
		t.Fatalf("Get() = (%v, %v), want successful cache hit", ok, err)
	}
	if err := hit.Body.Close(); err != nil {
		t.Fatal(err)
	}
}
