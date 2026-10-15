package cachegc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestPackagePlanKeepsWholeGAVUnderObjectBudgetAndDryRun(t *testing.T) {
	metadata, objects, inventory, groups, opts := packagePlanFixture(t)
	service := cachegc.NewCleanupService(metadata, objects)
	opts.MaxDeletes = 1
	opts.DryRun = true
	planned, err := service.ApplyCleanupPlan(t.Context(), inventory, groups, opts)
	if err != nil {
		t.Fatal(err)
	}
	if planned.EvictedPackages != 1 || len(planned.Candidates) != 2 {
		t.Fatalf("whole GAV was split by the object budget: %+v", planned)
	}
	preview := sweepPackagePlan(t, objects, planned, opts)
	if preview.DeletedBlobs != 2 {
		t.Fatalf("dry-run planned %d objects, want 2", preview.DeletedBlobs)
	}
	assertPackageReferences(t, metadata, 2)
	for _, digest := range groups[0].Digests {
		exists, existsErr := objects.Exists(t.Context(), digest)
		if existsErr != nil || !exists {
			t.Fatalf("dry-run removed %s: %v", digest, existsErr)
		}
	}
	opts.DryRun = false
	applied, err := service.ApplyCleanupPlan(t.Context(), inventory, groups, opts)
	if err != nil {
		t.Fatal(err)
	}
	actual := sweepPackagePlan(t, objects, applied, opts)
	if actual.DeletedBlobs != preview.DeletedBlobs {
		t.Fatalf("dry-run/actual mismatch: %d/%d", preview.DeletedBlobs, actual.DeletedBlobs)
	}
	assertPackageReferences(t, metadata, 0)
}

func TestPackageScanBudgetPrioritizesOldPackagesAcrossEcosystems(t *testing.T) {
	metadata, objects, inventory, groups, opts := packagePlanFixture(t)
	fresh := ecosystem.CacheCleanupGroup{
		Ecosystem: "aaa", Alias: "first", Package: "fresh", LastAccessAt: opts.Now,
	}
	unknown := ecosystem.CacheCleanupGroup{Ecosystem: "aaa", Alias: "first", Package: "unknown"}
	groups = append([]ecosystem.CacheCleanupGroup{unknown, fresh}, groups...)
	opts.MaxScan, opts.DryRun = 1, true
	plan, err := cachegc.NewCleanupService(metadata, objects).ApplyCleanupPlan(t.Context(), inventory, groups, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ScannedPackages != 1 || plan.EvictedPackages != 1 || len(plan.Candidates) != 2 {
		t.Fatalf("old package starved behind lexical prefix: %+v", plan)
	}
}

func TestPackagePlanRetainsEverySiblingAfterConcurrentAccess(t *testing.T) {
	metadata, objects, inventory, groups, opts := packagePlanFixture(t)
	manifest := inventory.Manifests[0]
	if err := artifactcache.Touch(t.Context(), metadata, manifest, opts.Now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	planned, err := cachegc.NewCleanupService(metadata, objects).ApplyCleanupPlan(t.Context(), inventory, groups, opts)
	if err != nil {
		t.Fatal(err)
	}
	if planned.EvictedPackages != 0 || len(planned.Candidates) != 0 {
		t.Fatalf("freshly accessed package was partially retired: %+v", planned)
	}
	assertPackageReferences(t, metadata, 2)
}

func TestPackagePlanPreservesMetadataOwnedByARetainedGroup(t *testing.T) {
	metadata, objects, inventory, groups, opts := packagePlanFixture(t)
	shared := groups[0]
	shared.Package = "another-package-owning-the-same-references"
	shared.LastAccessAt = opts.Now
	groups = append(groups, shared)
	planned, err := cachegc.NewCleanupService(metadata, objects).ApplyCleanupPlan(t.Context(), inventory, groups, opts)
	if err != nil {
		t.Fatal(err)
	}
	if planned.EvictedPackages != 1 {
		t.Fatalf("evicted packages=%d", planned.EvictedPackages)
	}
	assertPackageReferences(t, metadata, 2)
	if report := sweepPackagePlan(t, objects, planned, opts); report.DeletedBlobs != 0 {
		t.Fatalf("removed shared retained objects: %+v", report)
	}
}

func packagePlanFixture(t *testing.T) (meta.Store, object.Store, ecosystem.CacheCleanupInventory, []ecosystem.CacheCleanupGroup, ecosystem.CacheCleanupOptions) {
	t.Helper()
	metadata, objects := testStores(t)
	cache := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})
	for _, filename := range []string{"demo-1.0.jar", "demo-1.0.pom"} {
		entry, err := cache.Put(t.Context(), artifactcache.PutRequest{
			Key:       artifactcache.Key{Alias: "central", Repository: "com/example/demo/1.0", Reference: filename},
			AcceptKey: "maven", Body: strings.NewReader(filename), ContentType: "application/octet-stream",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := entry.Body.Close(); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := cachegc.LoadCleanupInventory(t.Context(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	opts := ecosystem.CacheCleanupOptions{Now: time.Now().UTC().Add(30 * 24 * time.Hour), UnusedFor: 7 * 24 * time.Hour}
	groups, err := artifactcache.PlanCacheCleanup(t.Context(), ecosystem.Maven, inventory, opts, func(repository, _ string) string { return repository })
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Tags) != 2 {
		t.Fatalf("unexpected Maven package plan: %+v", groups)
	}
	return metadata, objects, inventory, groups, opts
}

func sweepPackagePlan(t *testing.T, objects object.Store, plan *cachegc.PackageCleanupResult, opts ecosystem.CacheCleanupOptions) *cachegc.CleanupReport {
	t.Helper()
	report, err := cachegc.NewCleanupService(plan.Metadata, objects).CleanupBlobs(t.Context(), cachegc.CleanupOptions{
		Now: opts.Now, UnusedFor: opts.UnusedFor, DryRun: opts.DryRun, Candidates: plan.Candidates,
	})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func assertPackageReferences(t *testing.T, metadata meta.Store, want int) {
	t.Helper()
	tags, err := metadata.ListTags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tags.Len() != want {
		t.Fatalf("package references=%d, want %d", tags.Len(), want)
	}
}
