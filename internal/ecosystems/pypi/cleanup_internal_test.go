package pypi

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/stretchr/testify/require"
)

func TestPyPICleanupGroupsWheelMetadataAndSidecarsByFile(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	inventory := pypiCleanupRecords("pypi/packages", stale, map[string]string{
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl":           "sha256:wheel123",
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl.sha256":    "sha256:wheel123-sum",
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl.asc":       "sha256:wheel123-sig",
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl.metadata":  "sha256:wheel123-metadata",
		"https/files.pythonhosted.org/packages/demo-1.2.3.tar.gz":        "sha256:sdist123",
		"https/files.pythonhosted.org/packages/demo-1.2.3.tar.gz.sha512": "sha256:sdist123-sum",
		"https/files.pythonhosted.org/packages/demo-1.2.3.tar.gz.asc":    "sha256:sdist123-sig",
	})
	inventory = mergePyPICleanupRecords(inventory, pypiCleanupRecords("pypi/simple/demo", stale, map[string]string{
		"index.html": "sha256:project-index",
	}))
	// The wheel metadata file is still fresh, so the whole wheel family stays.
	markPyPIReferenceFresh(&inventory, "pypi/packages", "https/files.pythonhosted.org/packages/demo-1.2.3.whl.metadata", fresh)

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 3)
	wheel := pypiCleanupGroupForReference(t, groups, "https/files.pythonhosted.org/packages/demo-1.2.3.whl")
	require.True(t, wheel.LastAccessAt.Equal(fresh), "the fresh wheel metadata keeps its wheel family recent")
	require.ElementsMatch(t, []string{
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl",
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl.sha256",
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl.asc",
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl.metadata",
	}, pypiCleanupReferences(wheel))
	require.ElementsMatch(t, []string{
		"https/files.pythonhosted.org/packages/demo-1.2.3.tar.gz",
		"https/files.pythonhosted.org/packages/demo-1.2.3.tar.gz.sha512",
		"https/files.pythonhosted.org/packages/demo-1.2.3.tar.gz.asc",
	}, pypiCleanupReferences(pypiCleanupGroupForReference(t, groups, "https/files.pythonhosted.org/packages/demo-1.2.3.tar.gz")))
}

func TestPyPICleanupKeepsSimpleProjectIndexSeparateFromPackageFiles(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	inventory := mergePyPICleanupRecords(
		pypiCleanupRecords("pypi/simple/demo", stale, map[string]string{"index.html": "sha256:project-index"}),
		pypiCleanupRecords("pypi/packages", stale, map[string]string{
			"https/files.pythonhosted.org/packages/demo-1.2.3.whl": "sha256:wheel123",
		}),
	)

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 2)
	require.Contains(t, pypiCleanupGroupForReference(t, groups, "index.html").Package, "simple/demo")
	require.Contains(t, pypiCleanupGroupForReference(t, groups, "https/files.pythonhosted.org/packages/demo-1.2.3.whl").Package, "packages")
}

func TestPyPICleanupPreservesOpaqueQueryIdentityAcrossMetadataAndWheel(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	query := "?query-sha256=abc123"
	inventory := pypiCleanupRecords("pypi/packages", stale, map[string]string{
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl" + query:          "sha256:wheel",
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl.metadata" + query: "sha256:metadata",
	})

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.ElementsMatch(t, []string{
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl" + query,
		"https/files.pythonhosted.org/packages/demo-1.2.3.whl.metadata" + query,
	}, pypiCleanupReferences(groups[0]))
}

func pypiCleanupRecords(repository string, at time.Time, references map[string]string) ecosystem.CacheCleanupInventory {
	inventory := ecosystem.CacheCleanupInventory{}
	for reference, digest := range references {
		inventory.Manifests = append(inventory.Manifests, meta.ManifestRecord{
			Alias: "prod", Repository: repository, Reference: reference, Digest: digest, AcceptKey: acceptKeyPyPI,
			CreatedAt: at, UpdatedAt: at,
		})
		inventory.Tags = append(inventory.Tags, meta.TagRecord{
			Alias: "prod", Repository: repository, Reference: reference, Digest: digest, CreatedAt: at, UpdatedAt: at,
		})
		inventory.RepoBlobs = append(inventory.RepoBlobs, meta.RepoBlobRecord{
			Alias: "prod", Repository: repository, Digest: digest, CreatedAt: at, UpdatedAt: at, LastAccessAt: at,
		})
	}
	return inventory
}

func mergePyPICleanupRecords(inventories ...ecosystem.CacheCleanupInventory) ecosystem.CacheCleanupInventory {
	merged := ecosystem.CacheCleanupInventory{}
	for i := range inventories {
		merged.Manifests = append(merged.Manifests, inventories[i].Manifests...)
		merged.Tags = append(merged.Tags, inventories[i].Tags...)
		merged.RepoBlobs = append(merged.RepoBlobs, inventories[i].RepoBlobs...)
	}
	return merged
}

func pypiCleanupReferences(group ecosystem.CacheCleanupGroup) []string {
	references := make([]string, 0, len(group.Manifests))
	for i := range group.Manifests {
		references = append(references, group.Manifests[i].Reference)
	}
	return references
}

func pypiCleanupGroupForReference(t *testing.T, groups []ecosystem.CacheCleanupGroup, reference string) ecosystem.CacheCleanupGroup {
	t.Helper()
	for i := range groups {
		for j := range groups[i].Manifests {
			if groups[i].Manifests[j].Reference == reference {
				return groups[i]
			}
		}
	}
	t.Fatalf("cleanup plan missing PyPI reference %q", reference)
	return ecosystem.CacheCleanupGroup{}
}

func markPyPIReferenceFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time) {
	digest := ""
	markPyPITagFresh(inventory, repository, reference, at, &digest)
	markPyPIManifestFresh(inventory, repository, reference, at)
	markPyPIRepoBlobFresh(inventory, repository, digest, at)
}

func markPyPITagFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time, digest *string) {
	for i := range inventory.Tags {
		if inventory.Tags[i].Repository == repository && inventory.Tags[i].Reference == reference {
			inventory.Tags[i].UpdatedAt = at
			*digest = inventory.Tags[i].Digest
		}
	}
}

func markPyPIManifestFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time) {
	for i := range inventory.Manifests {
		if inventory.Manifests[i].Repository == repository && inventory.Manifests[i].Reference == reference {
			inventory.Manifests[i].UpdatedAt = at
		}
	}
}

func markPyPIRepoBlobFresh(inventory *ecosystem.CacheCleanupInventory, repository, digest string, at time.Time) {
	for i := range inventory.RepoBlobs {
		if inventory.RepoBlobs[i].Repository == repository && inventory.RepoBlobs[i].Digest == digest {
			inventory.RepoBlobs[i].LastAccessAt = at
		}
	}
}
