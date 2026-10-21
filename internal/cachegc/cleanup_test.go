package cachegc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestPruneReferencesExpiresImmutableArtifactsAcrossAliases(t *testing.T) {
	ctx := context.Background()
	metadata, objects := testStores(t)
	now := time.Now().UTC().Add(72 * time.Hour)
	digestValue := putBlob(ctx, t, metadata, objects, []byte("immutable jar"), now.Add(-72*time.Hour))
	_, err := metadata.UpsertManifest(ctx, meta.ManifestRecord{
		Alias: "maven-mirror", Repository: "org/example", Reference: "1.0", Digest: digestValue,
		MediaType: "application/java-archive", Size: int64(len("immutable jar")), ObjectKey: digestValue,
		CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-72 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = metadata.UpsertTag(ctx, meta.TagRecord{
		Alias: "maven-mirror", Repository: "org/example", Reference: "1.0", Digest: digestValue,
		CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-72 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	service := cachegc.New(metadata, objects)
	report, err := service.Cleanup(ctx, cachegc.CleanupOptions{UnusedFor: 24 * time.Hour, PruneReferences: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedBlobs != 1 {
		t.Fatalf("deleted %d blobs; want 1", report.DeletedBlobs)
	}
	manifests, err := metadata.ListManifests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := metadata.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if manifests.Len() != 0 || tags.Len() != 0 {
		t.Fatalf("stale refs remain: manifests=%d tags=%d", manifests.Len(), tags.Len())
	}
	assertMissing(ctx, t, objects, digestValue)
}

func TestPruneReferencesKeepsActiveSharedDigestAndOCIIndexGraph(t *testing.T) {
	ctx := context.Background()
	metadata, objects := testStores(t)
	now := time.Now().UTC().Add(72 * time.Hour)
	layer := putBlob(ctx, t, metadata, objects, []byte("layer"), now.Add(-72*time.Hour))
	config := putBlob(ctx, t, metadata, objects, []byte("config"), now.Add(-72*time.Hour))
	childBody, err := json.Marshal(ocispec.Manifest{MediaType: distribution.MediaTypeOCIManifest, Config: ocispec.Descriptor{Digest: digest.Digest(config)}, Layers: []ocispec.Descriptor{{Digest: digest.Digest(layer)}}})
	if err != nil {
		t.Fatal(err)
	}
	child := putBlob(ctx, t, metadata, objects, childBody, now.Add(-72*time.Hour))
	indexBody, err := json.Marshal(ocispec.Index{MediaType: distribution.MediaTypeOCIIndex, Manifests: []ocispec.Descriptor{{Digest: digest.Digest(child)}}})
	if err != nil {
		t.Fatal(err)
	}
	index := putBlob(ctx, t, metadata, objects, indexBody, now)
	putManifest(ctx, t, metadata, index, distribution.MediaTypeOCIIndex, now)
	putManifest(ctx, t, metadata, child, distribution.MediaTypeOCIManifest, now.Add(-72*time.Hour))
	// A current tag in another configured alias shares the same content digest.
	_, err = metadata.UpsertTag(ctx, meta.TagRecord{Alias: "active-mirror", Repository: "library/alpine", Reference: "latest", Digest: index, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = metadata.UpsertManifest(ctx, meta.ManifestRecord{Alias: "active-mirror", Repository: "library/alpine", Reference: "latest", Digest: index, MediaType: distribution.MediaTypeOCIIndex, ObjectKey: index, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, pullErr := metadata.RecordPull(ctx, meta.PullKey{Alias: "active-mirror", Repository: "library/alpine", Reference: "latest"}, now); pullErr != nil {
		t.Fatal(pullErr)
	}

	_, err = cachegc.New(metadata, objects).Cleanup(ctx, cachegc.CleanupOptions{UnusedFor: 24 * time.Hour, PruneReferences: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{index, child, config, layer} {
		assertExists(ctx, t, objects, value)
	}
}

func TestPruneReferencesDryRunDoesNotMutateReferencesOrObjects(t *testing.T) {
	ctx := context.Background()
	metadata, objects := testStores(t)
	now := time.Now().UTC().Add(72 * time.Hour)
	digestValue := putBlob(ctx, t, metadata, objects, []byte("old"), now.Add(-72*time.Hour))
	_, err := metadata.UpsertTag(ctx, meta.TagRecord{Alias: "npm", Repository: "pkg", Reference: "1.0", Digest: digestValue, CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-72 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	report, err := cachegc.New(metadata, objects).Cleanup(ctx, cachegc.CleanupOptions{UnusedFor: 24 * time.Hour, PruneReferences: true, DryRun: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedBlobs != 1 {
		t.Fatalf("planned deletions = %d; want 1", report.DeletedBlobs)
	}
	tags, err := metadata.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tags.Len() != 1 {
		t.Fatalf("dry-run mutated tags: %d remain", tags.Len())
	}
	assertExists(ctx, t, objects, digestValue)
}

func TestPruneReferencesRechecksBlobAccessBeforeDeleting(t *testing.T) {
	ctx := context.Background()
	metadata, objects := testStores(t)
	now := time.Now().UTC().Add(72 * time.Hour)
	digestValue := putBlob(ctx, t, metadata, objects, []byte("touched during scan"), now.Add(-72*time.Hour))
	calls := 0
	wrapped := touchAfterBlobSnapshot{Store: metadata, digest: digestValue, at: now.Add(time.Hour), calls: &calls}
	report, err := cachegc.NewCleanupService(wrapped, objects).CleanupBlobs(ctx, cachegc.CleanupOptions{UnusedFor: 24 * time.Hour, PruneReferences: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedBlobs != 0 || (report.RecentBlobs == 0 && report.ProtectedBlobs == 0) {
		t.Fatalf("updated blob was not skipped: %#v", report)
	}
	if calls < 2 {
		t.Fatal("cleanup did not recheck references after the initial snapshot")
	}
	assertExists(ctx, t, objects, digestValue)
}

func TestPruneReferencesPreservesReferencesWhenObjectDeleteFails(t *testing.T) {
	ctx := context.Background()
	metadata, objects := testStores(t)
	now := time.Now().UTC().Add(72 * time.Hour)
	digestValue := putBlob(ctx, t, metadata, objects, []byte("retry me"), now.Add(-72*time.Hour))
	_, err := metadata.UpsertTag(ctx, meta.TagRecord{Alias: "npm", Repository: "pkg", Reference: "1.0", Digest: digestValue, CreatedAt: now.Add(-72 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cachegc.NewCleanupService(metadata, failingDeleteStore{Store: objects}).CleanupBlobs(ctx, cachegc.CleanupOptions{UnusedFor: 24 * time.Hour, PruneReferences: true, Now: now})
	if err == nil {
		t.Fatal("expected object deletion failure")
	}
	tags, err := metadata.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tags.Len() != 1 {
		t.Fatalf("failed object delete pruned retry metadata: %d tags remain", tags.Len())
	}
	assertExists(ctx, t, objects, digestValue)
}

func testStores(t *testing.T) (meta.Store, object.Store) {
	t.Helper()
	metadata, err := meta.OpenSQLite(filepath.Join(t.TempDir(), "metadata.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	objects, err := object.NewLocal(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	return metadata, objects
}

func putBlob(ctx context.Context, t *testing.T, metadata meta.Store, objects object.Store, body []byte, at time.Time) string {
	t.Helper()
	d := digest.SHA256.FromBytes(body).String()
	info, err := objects.Put(ctx, d, bytes.NewReader(body), object.PutOptions{ContentType: distribution.MediaTypeOctetStream})
	if err != nil {
		t.Fatal(err)
	}
	_, err = metadata.UpsertBlob(ctx, meta.BlobRecord{Digest: d, Size: info.Size, MediaType: distribution.MediaTypeOctetStream, ObjectKey: d, CreatedAt: at, UpdatedAt: at, LastAccessAt: at})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func putManifest(ctx context.Context, t *testing.T, metadata meta.Store, d, mediaType string, at time.Time) {
	t.Helper()
	_, err := metadata.UpsertManifest(ctx, meta.ManifestRecord{Alias: "hub", Repository: "library/alpine", Reference: "latest", Digest: d, MediaType: mediaType, ObjectKey: d, CreatedAt: at, UpdatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
}

func assertExists(ctx context.Context, t *testing.T, objects object.Store, d string) {
	t.Helper()
	ok, err := objects.Exists(ctx, d)
	if err != nil || !ok {
		t.Fatalf("object %s exists=%v err=%v", d, ok, err)
	}
}

func assertMissing(ctx context.Context, t *testing.T, objects object.Store, d string) {
	t.Helper()
	ok, err := objects.Exists(ctx, d)
	if err != nil || ok {
		t.Fatalf("object %s exists=%v err=%v", d, ok, err)
	}
}

type touchAfterBlobSnapshot struct {
	meta.Store
	digest string
	at     time.Time
	calls  *int
}

func (s touchAfterBlobSnapshot) ReadCleanupSnapshot(ctx context.Context) (meta.CleanupSnapshot, error) {
	rows, err := meta.ReadCleanupSnapshot(ctx, s.Store)
	if err != nil {
		return meta.CleanupSnapshot{}, fmt.Errorf("read cleanup snapshot: %w", err)
	}
	*s.calls++
	// The initial bounded snapshot now combines the old blob and reference reads.
	// Touch after that snapshot so deletion must observe the newer access time.
	if *s.calls != 1 {
		return rows, nil
	}
	blob, ok, err := s.Blob(ctx, meta.BlobKey{Digest: s.digest})
	if err != nil {
		return rows, fmt.Errorf("read blob after snapshot: %w", err)
	}
	if !ok {
		return rows, errors.New("blob disappeared after snapshot")
	}
	blob.LastAccessAt = s.at
	_, err = s.UpsertBlob(ctx, *blob)
	if err != nil {
		return meta.CleanupSnapshot{}, fmt.Errorf("update blob access after snapshot: %w", err)
	}
	return rows, nil
}

type failingDeleteStore struct{ object.Store }

func (s failingDeleteStore) Delete(context.Context, string) error {
	return errors.New("injected delete failure")
}
