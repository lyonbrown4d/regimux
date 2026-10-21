package cachegc

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestManifestGraphBudgetCountsInitialRoots(t *testing.T) {
	ctx := meta.WithCleanupSnapshotLimits(context.Background(), meta.CleanupSnapshotLimits{
		MaxRecords: 1,
		MaxBytes:   100,
	})
	service := New(nil, nil)
	snapshot := meta.CleanupSnapshot{Manifests: []meta.ManifestRecord{{
		Digest: "root-digest", ObjectKey: "separate-object-key", MediaType: "opaque",
	}}}
	_, err := service.loadReferencesFromSnapshot(ctx, time.Now(), false, snapshot)
	if !errors.Is(err, meta.ErrCleanupSnapshotLimit) {
		t.Fatalf("load references error = %v, want cleanup snapshot limit", err)
	}
}

func TestManifestGraphBudgetDeduplicatesRepeatedAndCyclicReferences(t *testing.T) {
	root := "root-digest"
	child := "child-digest"
	rootBody := `{"schemaVersion":2,"manifests":[{"digest":"child-digest"},{"digest":"child-digest"}]}`
	childBody := `{"schemaVersion":2,"manifests":[{"digest":"root-digest"}]}`
	objects := graphObjectStore{bodies: map[string]string{"root-key": rootBody, "child-key": childBody}}
	service := New(nil, objects)
	now := time.Now().UTC()
	snapshot := meta.CleanupSnapshot{Manifests: []meta.ManifestRecord{
		{Digest: root, ObjectKey: "root-key", MediaType: distribution.MediaTypeOCIIndex, CreatedAt: now, UpdatedAt: now},
		{Digest: child, ObjectKey: "child-key", MediaType: distribution.MediaTypeOCIIndex, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour)},
	}}
	ctx := meta.WithCleanupSnapshotLimits(context.Background(), meta.CleanupSnapshotLimits{
		MaxRecords: 4,
		MaxBytes:   100,
	})
	refs, err := service.loadReferencesFromSnapshot(ctx, now.Add(-24*time.Hour), true, snapshot)
	if err != nil {
		t.Fatalf("load cyclic manifest graph: %v", err)
	}
	if refs.protected.Len() != 4 {
		t.Fatalf("protected graph nodes = %d, want root, root key, child, and child key", refs.protected.Len())
	}
	if !refs.protected.Contains(child) {
		t.Fatalf("child %q was not protected", child)
	}
}

func TestManifestGraphBudgetCountsChildBytes(t *testing.T) {
	child := strings.Repeat("c", 20)
	body := `{"schemaVersion":2,"manifests":[{"digest":"` + child + `"}]}`
	objects := graphObjectStore{bodies: map[string]string{"root-key": body}}
	service := New(nil, objects)
	protected := set.NewSet[string]()
	protected.Add("root")
	ctx := meta.WithCleanupSnapshotLimits(context.Background(), meta.CleanupSnapshotLimits{
		MaxRecords: 10,
		MaxBytes:   30,
	})
	err := service.protectManifestGraph(ctx, []meta.ManifestRecord{{
		Digest: "root", ObjectKey: "root-key", MediaType: distribution.MediaTypeOCIIndex,
	}}, protected, false)
	if !errors.Is(err, meta.ErrCleanupSnapshotLimit) {
		t.Fatalf("protect manifest graph error = %v, want cleanup snapshot limit", err)
	}
	if protected.Contains(child) {
		t.Fatalf("over-budget child %q was added to protected set", child)
	}
}

func TestManifestGraphQueuesNewObjectKeyWhenItNamesAnotherManifest(t *testing.T) {
	root := "root-digest"
	middle := "middle-digest"
	grandchild := "grandchild-digest"
	objects := graphObjectStore{bodies: map[string]string{
		"middle-key": `{"schemaVersion":2,"manifests":[{"digest":"grandchild-digest"}]}`,
	}}
	service := New(nil, objects)
	protected := set.NewSet[string]()
	protected.Add(root)
	ctx := meta.WithCleanupSnapshotLimits(context.Background(), meta.CleanupSnapshotLimits{
		MaxRecords: 10,
		MaxBytes:   200,
	})
	err := service.protectManifestGraph(ctx, []meta.ManifestRecord{
		{Digest: root, ObjectKey: middle, MediaType: "opaque"},
		{Digest: middle, ObjectKey: "middle-key", MediaType: distribution.MediaTypeOCIIndex},
	}, protected, false)
	if err != nil {
		t.Fatalf("protect manifest graph: %v", err)
	}
	if !protected.Contains(grandchild) {
		t.Fatalf("manifest descendant %q was not protected", grandchild)
	}
}

type graphObjectStore struct {
	bodies map[string]string
}

func (store graphObjectStore) Stat(context.Context, string) (*object.Info, error) {
	return nil, object.ErrNotFound
}
func (store graphObjectStore) Exists(context.Context, string) (bool, error) { return false, nil }
func (store graphObjectStore) Put(context.Context, string, io.Reader, object.PutOptions) (*object.Info, error) {
	return nil, errors.New("unexpected put")
}
func (store graphObjectStore) Delete(context.Context, string) error {
	return errors.New("unexpected delete")
}
func (store graphObjectStore) Get(_ context.Context, digest string, _ object.GetOptions) (io.ReadCloser, *object.Info, error) {
	body, ok := store.bodies[digest]
	if !ok {
		return nil, nil, object.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(body)), &object.Info{Digest: digest, Size: int64(len(body))}, nil
}
