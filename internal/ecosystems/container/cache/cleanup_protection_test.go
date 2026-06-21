package cache_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestCleanupServiceProtectsManifestReferencedBlobs(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	metadata, objects := newTestStores(t)

	configDigest := putCleanupBlob(ctx, t, metadata, objects, []byte("config"), now.Add(-48*time.Hour))
	layerDigest := putCleanupBlob(ctx, t, metadata, objects, []byte("layer"), now.Add(-48*time.Hour))
	orphanDigest := putCleanupBlob(ctx, t, metadata, objects, []byte("orphan"), now.Add(-48*time.Hour))
	putCleanupManifest(ctx, t, metadata, objects, distribution.MediaTypeOCIManifest, marshalCleanupJSON(t, ocispec.Manifest{
		MediaType: distribution.MediaTypeOCIManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    digest.Digest(configDigest),
		},
		Layers: []ocispec.Descriptor{{
			MediaType: ocispec.MediaTypeImageLayerGzip,
			Digest:    digest.Digest(layerDigest),
		}},
	}))

	report, err := cache.NewCleanupService(metadata, objects).CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor: 24 * time.Hour,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("cleanup blobs: %v", err)
	}
	if report.DeletedBlobs != 1 || report.DeletedDigests[0] != orphanDigest || report.ProtectedBlobs != 2 {
		t.Fatalf("unexpected cleanup report: %#v", report)
	}
	assertObjectsExist(ctx, t, objects, configDigest, layerDigest)
	assertObjectMissing(ctx, t, objects, orphanDigest)
}

func TestCleanupServiceProtectsIndexChildManifestBlob(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	metadata, objects := newTestStores(t)

	childBody := marshalCleanupJSON(t, ocispec.Manifest{MediaType: distribution.MediaTypeOCIManifest})
	childDigest := putCleanupBlob(ctx, t, metadata, objects, childBody, now.Add(-48*time.Hour))
	orphanDigest := putCleanupBlob(ctx, t, metadata, objects, []byte("orphan"), now.Add(-48*time.Hour))
	putCleanupManifest(ctx, t, metadata, objects, distribution.MediaTypeOCIIndex, marshalCleanupJSON(t, ocispec.Index{
		MediaType: distribution.MediaTypeOCIIndex,
		Manifests: []ocispec.Descriptor{{
			MediaType: distribution.MediaTypeOCIManifest,
			Digest:    digest.Digest(childDigest),
		}},
	}))

	report, err := cache.NewCleanupService(metadata, objects).CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor: 24 * time.Hour,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("cleanup blobs: %v", err)
	}
	if report.DeletedBlobs != 1 || report.DeletedDigests[0] != orphanDigest || report.ProtectedBlobs != 1 {
		t.Fatalf("unexpected cleanup report: %#v", report)
	}
	assertObjectsExist(ctx, t, objects, childDigest)
	assertObjectMissing(ctx, t, objects, orphanDigest)
}

func putCleanupManifest(
	ctx context.Context,
	t *testing.T,
	metadata meta.Store,
	objects object.Store,
	mediaType string,
	body []byte,
) {
	t.Helper()

	manifestDigest := testDigestFor(body)
	info, err := objects.Put(ctx, manifestDigest, bytes.NewReader(body), object.PutOptions{ContentType: mediaType})
	if err != nil {
		t.Fatalf("put cleanup manifest object: %v", err)
	}
	_, err = metadata.UpsertManifest(ctx, meta.ManifestRecord{
		Alias:      "hub",
		Repository: "library/alpine",
		Digest:     manifestDigest,
		MediaType:  mediaType,
		Size:       info.Size,
		ObjectKey:  info.Digest,
	})
	if err != nil {
		t.Fatalf("upsert cleanup manifest: %v", err)
	}
}

func marshalCleanupJSON(t *testing.T, value any) []byte {
	t.Helper()

	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal cleanup JSON: %v", err)
	}
	return body
}
