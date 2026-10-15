package scheduler

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestNightlySharedCleanupRegistersWithoutOCIRuntime(t *testing.T) {
	runtime, _, _ := newSharedCleanupRuntime(t)
	if err := runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if runtime.cleanupJob == nil {
		t.Fatal("global cleanup job missing without OCI runtime")
	}
	next, err := runtime.cleanupJob.NextRun()
	if err != nil {
		t.Fatal(err)
	}
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	next = next.In(zone)
	if next.Hour() != 3 || next.Minute() != 0 {
		t.Fatalf("cleanup next run=%s, want 03:00 Shanghai", next)
	}
	count := 0
	for _, job := range runtime.scheduler.Jobs() {
		if job.Name() == sharedCleanupJobName {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("registered cleanup jobs=%d, want one global job", count)
	}
}

func TestManualCleanupUsesSharedJobForEveryPackageEcosystem(t *testing.T) {
	runtime, metadata, objects := newSharedCleanupRuntime(t)
	digests := seedSharedCleanupArtifacts(t, metadata, objects)
	if err := runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.TriggerCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitForSharedCleanup(t, objects, digests)
}

func TestIntervalOverrideRunsSharedCleanupAutomatically(t *testing.T) {
	runtime, metadata, objects := newSharedCleanupRuntime(t)
	runtime.cfg.Scheduler.Cleanup.Interval = 20 * time.Millisecond
	digests := seedSharedCleanupArtifacts(t, metadata, objects)
	if err := runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitForSharedCleanup(t, objects, digests)
}

func TestManualSharedCleanupWorksWhenAutomaticSchedulerDisabled(t *testing.T) {
	runtime, metadata, objects := newSharedCleanupRuntime(t)
	runtime.cfg.Scheduler.Enabled = false
	digests := seedSharedCleanupArtifacts(t, metadata, objects)
	if err := runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.TriggerCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitForSharedCleanup(t, objects, digests)
}

func TestManualCleanupOutlivesRequestWhenNightlyJobDisabled(t *testing.T) {
	runtime, metadata, objects := newSharedCleanupRuntime(t)
	runtime.cfg.Scheduler.Cleanup.Enabled = false
	digests := seedSharedCleanupArtifacts(t, metadata, objects)
	if err := runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	requestCtx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runtime.TriggerCleanup(requestCtx); err != nil {
		t.Fatal(err)
	}
	waitForSharedCleanup(t, objects, digests)
}

func newSharedCleanupRuntime(t *testing.T) (*Runtime, meta.Store, object.Store) {
	t.Helper()
	metadata, err := meta.OpenSQLite(filepath.Join(t.TempDir(), "cache.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Scheduler.DistributedLock = false
	runtime := NewRuntime(RuntimeDependencies{
		Config: cfg, Metadata: metadata, Objects: objects,
		Runtimes: collectionlist.NewList[ecosystem.Runtime](testPackageCleanupPlanner{}),
	})
	future := time.Now().UTC().Add(30 * 24 * time.Hour)
	runtime.cleanupNow = func() time.Time { return future }
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := runtime.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return runtime, metadata, objects
}

type testPackageCleanupPlanner struct{}

func (testPackageCleanupPlanner) Name() string { return "test-package-planner" }

func (testPackageCleanupPlanner) PlanCacheCleanup(_ context.Context, inventory ecosystem.CacheCleanupInventory, _ ecosystem.CacheCleanupOptions) ([]ecosystem.CacheCleanupGroup, error) {
	groups := make([]ecosystem.CacheCleanupGroup, 0, len(inventory.Manifests))
	for i := range inventory.Manifests {
		manifest := inventory.Manifests[i]
		group := ecosystem.CacheCleanupGroup{
			Ecosystem: "test", Alias: manifest.Alias, Package: manifest.Repository + "/" + manifest.Reference,
			LastAccessAt: manifest.UpdatedAt, Manifests: []meta.ManifestRecord{manifest}, Digests: []string{manifest.Digest},
		}
		group.Tags, group.RepoBlobs = testPackageReferences(inventory, manifest)
		groups = append(groups, group)
	}
	return groups, nil
}

func testPackageReferences(inventory ecosystem.CacheCleanupInventory, manifest meta.ManifestRecord) ([]meta.TagRecord, []meta.RepoBlobRecord) {
	var tags []meta.TagRecord
	var links []meta.RepoBlobRecord
	key := meta.ManifestKey{Alias: manifest.Alias, Repository: manifest.Repository, Digest: manifest.Digest}
	for i := range inventory.Tags {
		tag := inventory.Tags[i]
		if key == (meta.ManifestKey{Alias: tag.Alias, Repository: tag.Repository, Digest: tag.Digest}) {
			tags = append(tags, tag)
		}
	}
	for i := range inventory.RepoBlobs {
		link := inventory.RepoBlobs[i]
		if key == (meta.ManifestKey{Alias: link.Alias, Repository: link.Repository, Digest: link.Digest}) {
			links = append(links, link)
		}
	}
	return tags, links
}

func seedSharedCleanupArtifacts(t *testing.T, metadata meta.Store, objects object.Store) []string {
	t.Helper()
	cache := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})
	digests := make([]string, 0, 6)
	for _, alias := range []string{"go/default", "npm/default", "pypi/default", "maven/default", "dist/default", "hub"} {
		entry, err := cache.Put(t.Context(), artifactcache.PutRequest{
			Key:  artifactcache.Key{Alias: alias, Repository: "demo", Reference: "1.0"},
			Body: bytes.NewReader([]byte("artifact-" + alias)), ContentType: "application/octet-stream",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, entry.Body); err != nil {
			t.Fatal(err)
		}
		if err := entry.Body.Close(); err != nil {
			t.Fatal(err)
		}
		digests = append(digests, entry.Digest)
	}
	return digests
}

func waitForSharedCleanup(t *testing.T, objects object.Store, digests []string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sharedCleanupObjectsMissing(t, objects, digests) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("shared cleanup did not remove every ecosystem's idle artifact")
}

func sharedCleanupObjectsMissing(t *testing.T, objects object.Store, digests []string) bool {
	t.Helper()
	for _, digest := range digests {
		exists, err := objects.Exists(t.Context(), digest)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			return false
		}
	}
	return true
}
