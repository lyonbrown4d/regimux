package meta_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

const concurrentMetadataWriters = 32

func TestSQLStoreUpsertManifestConcurrent(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)
	expires := time.Now().UTC().Add(time.Hour)

	runConcurrentMetadataWrites(t, func() error {
		_, err := store.UpsertManifest(ctx, meta.ManifestRecord{
			Alias:      "hub",
			Repository: "library/busybox",
			Digest:     testDigest,
			MediaType:  distribution.MediaTypeOCIManifest,
			Size:       610,
			ExpiresAt:  expires,
		})
		if err != nil {
			return fmt.Errorf("upsert manifest: %w", err)
		}
		return nil
	})

	manifests, err := store.ListManifests(ctx)
	requireNoError(t, "list manifests after concurrent upsert", err)
	if len(manifests) != 1 || manifests[0].Digest != testDigest {
		t.Fatalf("unexpected manifests after concurrent upsert: %#v", manifests)
	}
}

func TestSQLStoreUpsertBlobConcurrent(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)

	runConcurrentMetadataWrites(t, func() error {
		_, err := store.UpsertBlob(ctx, meta.BlobRecord{
			Digest:    testDigest,
			Size:      459,
			MediaType: distribution.MediaTypeOctetStream,
			ObjectKey: testDigest,
		})
		if err != nil {
			return fmt.Errorf("upsert blob: %w", err)
		}
		return nil
	})

	blobs, err := store.ListBlobs(ctx)
	requireNoError(t, "list blobs after concurrent upsert", err)
	if len(blobs) != 1 || blobs[0].Digest != testDigest {
		t.Fatalf("unexpected blobs after concurrent upsert: %#v", blobs)
	}
}

func TestSQLStoreUpsertTagConcurrent(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)
	expires := time.Now().UTC().Add(time.Hour)

	runConcurrentMetadataWrites(t, func() error {
		_, err := store.UpsertTag(ctx, meta.TagRecord{
			Alias:      "hub",
			Repository: "library/busybox",
			Reference:  "1.36.1",
			Digest:     testDigest,
			ExpiresAt:  expires,
		})
		if err != nil {
			return fmt.Errorf("upsert tag: %w", err)
		}
		return nil
	})

	tags, err := store.ListTags(ctx)
	requireNoError(t, "list tags after concurrent upsert", err)
	if len(tags) != 1 || tags[0].Reference != "1.36.1" {
		t.Fatalf("unexpected tags after concurrent upsert: %#v", tags)
	}
}

func TestSQLStoreUpsertRepoBlobConcurrent(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)

	runConcurrentMetadataWrites(t, func() error {
		_, err := store.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
			Alias:          "hub",
			Repository:     "library/busybox",
			Digest:         testDigest,
			SourceManifest: secondTestDigest,
		})
		if err != nil {
			return fmt.Errorf("upsert repository blob: %w", err)
		}
		return nil
	})

	repoBlobs, err := store.ListRepoBlobs(ctx)
	requireNoError(t, "list repository blobs after concurrent upsert", err)
	if len(repoBlobs) != 1 || repoBlobs[0].Digest != testDigest {
		t.Fatalf("unexpected repository blobs after concurrent upsert: %#v", repoBlobs)
	}
}

func runConcurrentMetadataWrites(t *testing.T, write func() error) {
	t.Helper()

	start := make(chan struct{})
	errs := make(chan error, concurrentMetadataWriters)
	var wg sync.WaitGroup
	wg.Add(concurrentMetadataWriters)
	for range concurrentMetadataWriters {
		go func() {
			defer wg.Done()
			<-start
			if err := write(); err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		requireNoError(t, "concurrent metadata write", err)
	}
}
