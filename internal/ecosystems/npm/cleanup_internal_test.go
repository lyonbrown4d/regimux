package npm

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/stretchr/testify/require"
)

func TestNPMCleanupKeepsCatalogSeparateAndGroupsTarballSidecars(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	inventory := npmCleanupRecords("registry", "@acme/demo", stale, map[string]string{
		"metadata":                      "sha256:catalog",
		"tarball:demo-1.2.3.tgz":        "sha256:tarball123",
		"tarball:demo-1.2.3.tgz.sha256": "sha256:tarball123-sum",
		"tarball:demo-1.2.3.tgz.asc":    "sha256:tarball123-sig",
		"tarball:demo-2.0.0.tgz":        "sha256:tarball200",
	})
	// One active sidecar keeps the full version tarball family, while the other version remains eligible.
	markNPMReferenceFresh(&inventory, "@acme/demo", "tarball:demo-1.2.3.tgz.sha256", fresh)

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 3, "the catalog and each version tarball are separate groups")
	require.ElementsMatch(t, []string{"metadata"}, npmCleanupReferences(npmCleanupGroupForReference(t, groups, "metadata")))
	freshTarball := npmCleanupGroupForReference(t, groups, "tarball:demo-1.2.3.tgz")
	require.True(t, freshTarball.LastAccessAt.Equal(fresh), "one fresh sidecar protects the complete tarball family")
	require.ElementsMatch(t, []string{
		"tarball:demo-1.2.3.tgz", "tarball:demo-1.2.3.tgz.sha256", "tarball:demo-1.2.3.tgz.asc",
	}, npmCleanupReferences(freshTarball))
	require.ElementsMatch(t, []string{"tarball:demo-2.0.0.tgz"}, npmCleanupReferences(npmCleanupGroupForReference(t, groups, "tarball:demo-2.0.0.tgz")))
}

func npmCleanupRecords(alias, repository string, at time.Time, references map[string]string) ecosystem.CacheCleanupInventory {
	inventory := ecosystem.CacheCleanupInventory{}
	for reference, digest := range references {
		inventory.Manifests = append(inventory.Manifests, meta.ManifestRecord{
			Alias: alias, Repository: repository, Reference: reference, Digest: digest, AcceptKey: ecosystemNPM,
			CreatedAt: at, UpdatedAt: at,
		})
		inventory.Tags = append(inventory.Tags, meta.TagRecord{
			Alias: alias, Repository: repository, Reference: reference, Digest: digest, CreatedAt: at, UpdatedAt: at,
		})
		inventory.RepoBlobs = append(inventory.RepoBlobs, meta.RepoBlobRecord{
			Alias: alias, Repository: repository, Digest: digest, CreatedAt: at, UpdatedAt: at, LastAccessAt: at,
		})
	}
	return inventory
}

func npmCleanupReferences(group ecosystem.CacheCleanupGroup) []string {
	references := make([]string, 0, len(group.Tags))
	for i := range group.Tags {
		references = append(references, group.Tags[i].Reference)
	}
	return references
}

func npmCleanupGroupForReference(t *testing.T, groups []ecosystem.CacheCleanupGroup, reference string) ecosystem.CacheCleanupGroup {
	t.Helper()
	for i := range groups {
		for j := range groups[i].Tags {
			if groups[i].Tags[j].Reference == reference {
				return groups[i]
			}
		}
	}
	t.Fatalf("cleanup plan missing npm reference %q", reference)
	return ecosystem.CacheCleanupGroup{}
}

func markNPMReferenceFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time) {
	digest := ""
	markNPMTagFresh(inventory, repository, reference, at, &digest)
	markNPMManifestFresh(inventory, repository, reference, at)
	markNPMRepoBlobFresh(inventory, repository, digest, at)
}

func markNPMTagFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time, digest *string) {
	for i := range inventory.Tags {
		if inventory.Tags[i].Repository == repository && inventory.Tags[i].Reference == reference {
			inventory.Tags[i].UpdatedAt = at
			*digest = inventory.Tags[i].Digest
		}
	}
}

func markNPMManifestFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time) {
	for i := range inventory.Manifests {
		if inventory.Manifests[i].Repository == repository && inventory.Manifests[i].Reference == reference {
			inventory.Manifests[i].UpdatedAt = at
		}
	}
}

func markNPMRepoBlobFresh(inventory *ecosystem.CacheCleanupInventory, repository, digest string, at time.Time) {
	for i := range inventory.RepoBlobs {
		if inventory.RepoBlobs[i].Repository == repository && inventory.RepoBlobs[i].Digest == digest {
			inventory.RepoBlobs[i].LastAccessAt = at
		}
	}
}
