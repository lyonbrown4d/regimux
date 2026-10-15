package cachegc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestPackageInvalidationFailurePreservesDurableCache(t *testing.T) {
	metadata, objects, inventory, groups, opts := packagePlanFixture(t)
	want := errors.New("transient cache unavailable")
	called := 0
	service := cachegc.NewCleanupService(metadata, objects, cachegc.WithPackageInvalidator(
		func(_ context.Context, group ecosystem.CacheCleanupGroup) error {
			called++
			assertPackageReferences(t, metadata, 2)
			if len(group.Tags) != 2 || len(group.Manifests) != 2 {
				t.Fatalf("incomplete invalidation: %+v", group)
			}
			return want
		},
	))
	if _, err := service.ApplyCleanupPlan(t.Context(), inventory, groups, opts); !errors.Is(err, want) {
		t.Fatalf("invalidation error = %v, want %v", err, want)
	}
	if called != 1 {
		t.Fatalf("invalidation calls = %d", called)
	}
	assertPackageReferences(t, metadata, 2)
	for _, digest := range groups[0].Digests {
		if exists, err := objects.Exists(t.Context(), digest); err != nil || !exists {
			t.Fatalf("invalidation failure removed object %s: %v", digest, err)
		}
	}
}

func TestPackageInvalidationExcludesRetainedSharedReferences(t *testing.T) {
	metadata, objects, inventory, groups, opts := packagePlanFixture(t)
	kept := groups[0]
	kept.Package = "active-shared-reference"
	kept.LastAccessAt = opts.Now
	kept.Tags = kept.Tags[:1]
	kept.Manifests, kept.RepoBlobs = nil, nil
	kept.Digests = []string{kept.Tags[0].Digest}
	for _, record := range groups[0].Manifests {
		if record.Digest == kept.Tags[0].Digest {
			kept.Manifests = append(kept.Manifests, record)
		}
	}
	for _, record := range groups[0].RepoBlobs {
		if record.Digest == kept.Tags[0].Digest {
			kept.RepoBlobs = append(kept.RepoBlobs, record)
		}
	}
	groups = append(groups, kept)
	called := 0
	service := cachegc.NewCleanupService(metadata, objects, cachegc.WithPackageInvalidator(
		func(_ context.Context, group ecosystem.CacheCleanupGroup) error {
			called++
			assertPackageReferences(t, metadata, 2)
			if len(group.Tags) != 1 || group.Tags[0].Reference == kept.Tags[0].Reference {
				t.Fatalf("retained shared tag was invalidated: %+v", group.Tags)
			}
			if len(group.Manifests) != 1 || group.Manifests[0].Digest == kept.Manifests[0].Digest {
				t.Fatalf("retained shared manifest was invalidated: %+v", group.Manifests)
			}
			return nil
		},
	))
	if _, err := service.ApplyCleanupPlan(t.Context(), inventory, groups, opts); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("invalidation calls = %d", called)
	}
	assertPackageReferences(t, metadata, 1)
}

func TestPackageInvalidationDoesNotRunInDryRun(t *testing.T) {
	metadata, objects, inventory, groups, opts := packagePlanFixture(t)
	opts.DryRun = true
	service := cachegc.NewCleanupService(metadata, objects, cachegc.WithPackageInvalidator(
		func(context.Context, ecosystem.CacheCleanupGroup) error {
			t.Fatal("dry-run invalidated a protocol cache")
			return nil
		},
	))
	if _, err := service.ApplyCleanupPlan(t.Context(), inventory, groups, opts); err != nil {
		t.Fatal(err)
	}
	assertPackageReferences(t, metadata, 2)
}

func TestOrphanInvalidationFailurePreservesEveryDurableReference(t *testing.T) {
	metadata, objects, _, groups, opts := packagePlanFixture(t)
	old := opts.Now.Add(-30 * 24 * time.Hour)
	if _, err := metadata.UpsertRepoBlob(t.Context(), meta.RepoBlobRecord{
		Alias: "orphan", Repository: "unowned", Digest: groups[0].Digests[0], LastAccessAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, err := metadata.UpsertBlob(t.Context(), meta.BlobRecord{
		Digest: digest, ObjectKey: digest, Size: 1, LastAccessAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	inventory, err := cachegc.LoadCleanupInventory(t.Context(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("orphan cache unavailable")
	orphanCalled := false
	service := cachegc.NewCleanupService(metadata, objects, cachegc.WithPackageInvalidator(
		func(_ context.Context, group ecosystem.CacheCleanupGroup) error {
			if group.Ecosystem != "" {
				return nil
			}
			orphanCalled = true
			if len(group.RepoBlobs) != 1 || group.RepoBlobs[0].Repository != "unowned" || len(group.Digests) != 1 || group.Digests[0] != digest {
				t.Fatalf("incomplete orphan invalidation: %+v", group)
			}
			return want
		},
	))
	if _, err := service.ApplyCleanupPlan(t.Context(), inventory, groups, opts); !errors.Is(err, want) {
		t.Fatalf("orphan invalidation error = %v", err)
	}
	if !orphanCalled {
		t.Fatal("orphan transient cache was not invalidated")
	}
	assertPackageReferences(t, metadata, 2)
}
