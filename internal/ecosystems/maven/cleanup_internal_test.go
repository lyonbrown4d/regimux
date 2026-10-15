package maven

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/stretchr/testify/require"
)

func TestMavenCleanupKeepsEachGAVAsASeparateGroupWhenDigestsAreShared(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	first := mavenCleanupRecords("org/acme/demo/1.2.3", stale, map[string]string{
		"demo-1.2.3.jar":        "sha256:jar123",
		"demo-1.2.3.pom":        "sha256:pom123",
		"demo-1.2.3.pom.sha256": "sha256:shared-checksum",
		"demo-1.2.3.pom.asc":    "sha256:signature123",
	})
	second := mavenCleanupRecords("org/acme/demo/2.0.0", stale, map[string]string{
		"demo-2.0.0.jar":        "sha256:jar200",
		"demo-2.0.0.pom":        "sha256:pom200",
		"demo-2.0.0.pom.sha256": "sha256:shared-checksum",
	})
	inventory := mergeMavenCleanupRecords(first, second)
	inventory.Manifests = append(inventory.Manifests, meta.ManifestRecord{
		Alias: "central", Repository: "org/acme/demo/1.2.3", Reference: "demo-1.2.3.other", Digest: "sha256:not-maven",
		AcceptKey: "go", CreatedAt: stale, UpdatedAt: stale,
	})
	inventory.Tags = append(inventory.Tags, meta.TagRecord{
		Alias: "central", Repository: "org/acme/demo/1.2.3", Reference: "demo-1.2.3.other", Digest: "sha256:not-maven", CreatedAt: stale, UpdatedAt: stale,
	})
	inventory.RepoBlobs = append(inventory.RepoBlobs, meta.RepoBlobRecord{
		Alias: "central", Repository: "org/acme/demo/1.2.3", Digest: "sha256:not-maven", CreatedAt: stale, UpdatedAt: stale, LastAccessAt: stale,
	})

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 2)
	require.ElementsMatch(t, []string{"org/acme/demo/1.2.3", "org/acme/demo/2.0.0"}, []string{groups[0].Package, groups[1].Package})
	firstGroup := mavenCleanupGroupFor(t, groups, "org/acme/demo/1.2.3")
	secondGroup := mavenCleanupGroupFor(t, groups, "org/acme/demo/2.0.0")
	require.ElementsMatch(t, []string{
		"demo-1.2.3.jar", "demo-1.2.3.pom", "demo-1.2.3.pom.sha256", "demo-1.2.3.pom.asc",
	}, mavenCleanupReferences(firstGroup))
	require.ElementsMatch(t, []string{
		"demo-2.0.0.jar", "demo-2.0.0.pom", "demo-2.0.0.pom.sha256",
	}, mavenCleanupReferences(secondGroup))
	require.Contains(t, firstGroup.Digests, "sha256:shared-checksum")
	require.Contains(t, secondGroup.Digests, "sha256:shared-checksum")
	require.NotContains(t, mavenCleanupReferences(firstGroup), "demo-1.2.3.other")
}

func TestMavenCleanupFreshSiblingRetainsWholeGAV(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	oldVersion := mavenCleanupRecords("org/acme/demo/1.2.3", stale, map[string]string{
		"demo-1.2.3.jar":        "sha256:jar123",
		"demo-1.2.3.pom":        "sha256:pom123",
		"demo-1.2.3.pom.sha256": "sha256:sum123",
	})
	otherVersion := mavenCleanupRecords("org/acme/demo/2.0.0", stale, map[string]string{
		"demo-2.0.0.jar": "sha256:jar200",
	})
	inventory := mergeMavenCleanupRecords(oldVersion, mavenCleanupRecords("org/acme/demo/1.2.3", stale, map[string]string{
		"demo-1.2.3.pom.asc": "sha256:signature123",
	}), otherVersion)
	markMavenReferenceFresh(&inventory, "org/acme/demo/1.2.3", "demo-1.2.3.pom.asc", fresh)

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 2)
	freshGroup := mavenCleanupGroupFor(t, groups, "org/acme/demo/1.2.3")
	require.True(t, freshGroup.LastAccessAt.Equal(fresh), "the fresh signature makes the complete GAV group recent")
	require.ElementsMatch(t, []string{
		"demo-1.2.3.jar", "demo-1.2.3.pom", "demo-1.2.3.pom.sha256", "demo-1.2.3.pom.asc",
	}, mavenCleanupReferences(freshGroup))
	otherGroup := mavenCleanupGroupFor(t, groups, "org/acme/demo/2.0.0")
	require.True(t, otherGroup.LastAccessAt.Before(now.Add(-7*24*time.Hour)))
}

func mavenCleanupRecords(repository string, at time.Time, references map[string]string) ecosystem.CacheCleanupInventory {
	inventory := ecosystem.CacheCleanupInventory{}
	for reference, digest := range references {
		inventory.Manifests = append(inventory.Manifests, meta.ManifestRecord{
			Alias: "central", Repository: repository, Reference: reference, Digest: digest, AcceptKey: acceptKeyMaven, CreatedAt: at, UpdatedAt: at,
		})
		inventory.Tags = append(inventory.Tags, meta.TagRecord{
			Alias: "central", Repository: repository, Reference: reference, Digest: digest, CreatedAt: at, UpdatedAt: at,
		})
		inventory.RepoBlobs = append(inventory.RepoBlobs, meta.RepoBlobRecord{
			Alias: "central", Repository: repository, Digest: digest, CreatedAt: at, UpdatedAt: at, LastAccessAt: at,
		})
	}
	return inventory
}

func mergeMavenCleanupRecords(inventories ...ecosystem.CacheCleanupInventory) ecosystem.CacheCleanupInventory {
	merged := ecosystem.CacheCleanupInventory{}
	for i := range inventories {
		merged.Manifests = append(merged.Manifests, inventories[i].Manifests...)
		merged.Tags = append(merged.Tags, inventories[i].Tags...)
		merged.RepoBlobs = append(merged.RepoBlobs, inventories[i].RepoBlobs...)
	}
	return merged
}

func mavenCleanupGroupFor(t *testing.T, groups []ecosystem.CacheCleanupGroup, packageName string) ecosystem.CacheCleanupGroup {
	t.Helper()
	for i := range groups {
		if groups[i].Package == packageName {
			return groups[i]
		}
	}
	t.Fatalf("cleanup plan missing package group %q", packageName)
	return ecosystem.CacheCleanupGroup{}
}

func mavenCleanupReferences(group ecosystem.CacheCleanupGroup) []string {
	references := make([]string, 0, len(group.Manifests))
	for i := range group.Manifests {
		references = append(references, group.Manifests[i].Reference)
	}
	return references
}

func markMavenReferenceFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time) {
	digest := ""
	markMavenTagFresh(inventory, repository, reference, at, &digest)
	markMavenManifestFresh(inventory, repository, reference, at)
	markMavenRepoBlobFresh(inventory, repository, digest, at)
}

func markMavenTagFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time, digest *string) {
	for i := range inventory.Tags {
		if inventory.Tags[i].Repository == repository && inventory.Tags[i].Reference == reference {
			inventory.Tags[i].UpdatedAt = at
			*digest = inventory.Tags[i].Digest
		}
	}
}

func markMavenManifestFresh(inventory *ecosystem.CacheCleanupInventory, repository, reference string, at time.Time) {
	for i := range inventory.Manifests {
		if inventory.Manifests[i].Repository == repository && inventory.Manifests[i].Reference == reference {
			inventory.Manifests[i].UpdatedAt = at
		}
	}
}

func markMavenRepoBlobFresh(inventory *ecosystem.CacheCleanupInventory, repository, digest string, at time.Time) {
	for i := range inventory.RepoBlobs {
		if inventory.RepoBlobs[i].Repository == repository && inventory.RepoBlobs[i].Digest == digest {
			inventory.RepoBlobs[i].LastAccessAt = at
		}
	}
}
