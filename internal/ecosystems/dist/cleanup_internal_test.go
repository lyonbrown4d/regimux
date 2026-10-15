package dist

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/stretchr/testify/require"
)

func TestDistCleanupGroupsKnownSidecarsWithBinaryButKeepsOtherFilesIndependent(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	inventory := distCleanupRecords("gradle", "releases/8.7", stale, map[string]string{
		"gradle-8.7-bin.tar.gz":        "sha256:archive",
		"gradle-8.7-bin.tar.gz.sha256": "sha256:checksum",
		"gradle-8.7-bin.tar.gz.asc":    "sha256:signature",
		"gradle-8.7-bin.zip":           "sha256:zip",
	})
	// A fresh signature protects its archive and checksum siblings, but not a different binary file.
	markDistReferenceFresh(&inventory, "releases/8.7", "gradle-8.7-bin.tar.gz.asc", fresh)

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 2)
	freshArchive := distCleanupGroupForReference(t, groups, "gradle-8.7-bin.tar.gz")
	require.True(t, freshArchive.LastAccessAt.Equal(fresh), "a fresh signature protects its archive family")
	require.ElementsMatch(t, []string{
		"gradle-8.7-bin.tar.gz", "gradle-8.7-bin.tar.gz.sha256", "gradle-8.7-bin.tar.gz.asc",
	}, distCleanupReferences(freshArchive))
	require.ElementsMatch(t, []string{"gradle-8.7-bin.zip"}, distCleanupReferences(distCleanupGroupForReference(t, groups, "gradle-8.7-bin.zip")))
}

func distCleanupRecords(alias, repository string, at time.Time, references map[string]string) ecosystem.CacheCleanupInventory {
	inventory := ecosystem.CacheCleanupInventory{}
	for reference, digest := range references {
		inventory.Manifests = append(inventory.Manifests, meta.ManifestRecord{
			Alias: alias, Repository: repository, Reference: reference, Digest: digest, AcceptKey: acceptKeyDist,
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

func distCleanupReferences(group ecosystem.CacheCleanupGroup) []string {
	references := make([]string, 0, len(group.Tags))
	for i := range group.Tags {
		references = append(references, group.Tags[i].Reference)
	}
	return references
}

func distCleanupGroupForReference(t *testing.T, groups []ecosystem.CacheCleanupGroup, reference string) ecosystem.CacheCleanupGroup {
	t.Helper()
	for i := range groups {
		for j := range groups[i].Tags {
			if groups[i].Tags[j].Reference == reference {
				return groups[i]
			}
		}
	}
	t.Fatalf("cleanup plan missing dist reference %q", reference)
	return ecosystem.CacheCleanupGroup{}
}

func markDistReferenceFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time) {
	digest := ""
	markDistTagFresh(inventory, repository, reference, at, &digest)
	markDistManifestFresh(inventory, repository, reference, at)
	markDistRepoBlobFresh(inventory, repository, digest, at)
}

func markDistTagFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time, digest *string) {
	for i := range inventory.Tags {
		if inventory.Tags[i].Repository == repository && inventory.Tags[i].Reference == reference {
			inventory.Tags[i].UpdatedAt = at
			*digest = inventory.Tags[i].Digest
		}
	}
}

func markDistManifestFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time) {
	for i := range inventory.Manifests {
		if inventory.Manifests[i].Repository == repository && inventory.Manifests[i].Reference == reference {
			inventory.Manifests[i].UpdatedAt = at
		}
	}
}

func markDistRepoBlobFresh(inventory *ecosystem.CacheCleanupInventory, repository, digest string, at time.Time) {
	for i := range inventory.RepoBlobs {
		if inventory.RepoBlobs[i].Repository == repository && inventory.RepoBlobs[i].Digest == digest {
			inventory.RepoBlobs[i].LastAccessAt = at
		}
	}
}
