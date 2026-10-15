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
		func(ctx context.Context, group ecosystem.CacheCleanupGroup) error {
			called++
			assertPackageReferences(ctx, t, metadata, 2)
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
	assertPackageReferences(t.Context(), t, metadata, 2)
	for _, digest := range groups[0].Digests {
		if exists, err := objects.Exists(t.Context(), digest); err != nil || !exists {
			t.Fatalf("invalidation failure removed object %s: %v", digest, err)
		}
	}
}

func TestPackageInvalidationExcludesRetainedSharedReferences(t *testing.T) {
	metadata, objects, inventory, groups, opts := packagePlanFixture(t)
	kept := retainedSharedPackage(groups[0], opts.Now)
	groups = append(groups, kept)
	called := 0
	service := cachegc.NewCleanupService(metadata, objects, cachegc.WithPackageInvalidator(
		func(ctx context.Context, group ecosystem.CacheCleanupGroup) error {
			called++
			assertPackageReferences(ctx, t, metadata, 2)
			assertOnlyRetiredPackageReferences(t, group, kept)
			return nil
		},
	))
	if _, err := service.ApplyCleanupPlan(t.Context(), inventory, groups, opts); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("invalidation calls = %d", called)
	}
	assertPackageReferences(t.Context(), t, metadata, 1)
}

func retainedSharedPackage(source ecosystem.CacheCleanupGroup, now time.Time) ecosystem.CacheCleanupGroup {
	kept := source
	kept.Package = "active-shared-reference"
	kept.LastAccessAt = now
	kept.Tags = kept.Tags[:1]
	kept.Manifests = nil
	kept.RepoBlobs = nil
	kept.Digests = []string{kept.Tags[0].Digest}
	for i := range source.Manifests {
		if source.Manifests[i].Digest == kept.Tags[0].Digest {
			kept.Manifests = append(kept.Manifests, source.Manifests[i])
		}
	}
	for i := range source.RepoBlobs {
		if source.RepoBlobs[i].Digest == kept.Tags[0].Digest {
			kept.RepoBlobs = append(kept.RepoBlobs, source.RepoBlobs[i])
		}
	}
	return kept
}

func assertOnlyRetiredPackageReferences(t *testing.T, group, kept ecosystem.CacheCleanupGroup) {
	t.Helper()
	if len(group.Tags) != 1 || group.Tags[0].Reference == kept.Tags[0].Reference {
		t.Fatalf("retained shared tag was invalidated: %+v", group.Tags)
	}
	if len(group.Manifests) != 1 || group.Manifests[0].Digest == kept.Manifests[0].Digest {
		t.Fatalf("retained shared manifest was invalidated: %+v", group.Manifests)
	}
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
	assertPackageReferences(t.Context(), t, metadata, 2)
}

func TestOrphanInvalidationFailurePreservesEveryDurableReference(t *testing.T) {
	metadata, objects, _, groups, opts := packagePlanFixture(t)
	old := opts.Now.Add(-30 * 24 * time.Hour)
	inventory, digest := seedOrphanCleanupInventory(t.Context(), t, metadata, groups[0].Digests[0], old)
	want := errors.New("orphan cache unavailable")
	orphanCalled := false
	service := cachegc.NewCleanupService(metadata, objects, cachegc.WithPackageInvalidator(
		func(ctx context.Context, group ecosystem.CacheCleanupGroup) error {
			if group.Ecosystem != "" {
				return nil
			}
			orphanCalled = true
			assertPackageReferences(ctx, t, metadata, 2)
			assertOrphanInvalidationGroup(t, group, digest)
			return want
		},
	))
	if _, err := service.ApplyCleanupPlan(t.Context(), inventory, groups, opts); !errors.Is(err, want) {
		t.Fatalf("orphan invalidation error = %v", err)
	}
	if !orphanCalled {
		t.Fatal("orphan transient cache was not invalidated")
	}
	assertPackageReferences(t.Context(), t, metadata, 2)
}

func seedOrphanCleanupInventory(ctx context.Context, t *testing.T, metadata meta.Store, linkedDigest string, old time.Time) (ecosystem.CacheCleanupInventory, string) {
	t.Helper()
	if _, err := metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias: "orphan", Repository: "unowned", Digest: linkedDigest, LastAccessAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, err := metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest: digest, ObjectKey: digest, Size: 1, LastAccessAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	inventory, err := cachegc.LoadCleanupInventory(ctx, metadata)
	if err != nil {
		t.Fatal(err)
	}
	return inventory, digest
}

func assertOrphanInvalidationGroup(t *testing.T, group ecosystem.CacheCleanupGroup, digest string) {
	t.Helper()
	if len(group.RepoBlobs) != 1 || group.RepoBlobs[0].Repository != "unowned" {
		t.Fatalf("incomplete orphan links: %+v", group.RepoBlobs)
	}
	if len(group.Digests) != 1 || group.Digests[0] != digest {
		t.Fatalf("incomplete orphan digests: %+v", group.Digests)
	}
}
