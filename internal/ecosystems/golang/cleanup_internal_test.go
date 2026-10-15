package golang

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/stretchr/testify/require"
)

func TestGoCleanupGroupsVersionFilesAndKeepsVersionsIndependent(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	inventory := goCleanupRecords(stale, map[string]string{
		"@v/v1.2.3.zip":  "sha256:zip123",
		"@v/v1.2.3.mod":  "sha256:mod123",
		"@v/v1.2.3.info": "sha256:info123",
	})
	inventory = mergeGoCleanupRecords(inventory, goCleanupRecords(stale, map[string]string{
		"@v/v2.0.0.zip":  "sha256:zip200",
		"@v/v2.0.0.mod":  "sha256:mod200",
		"@v/v2.0.0.info": "sha256:info200",
	}))

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 2)
	first := goCleanupGroupForVersion(t, groups, "v1.2.3")
	second := goCleanupGroupForVersion(t, groups, "v2.0.0")
	require.ElementsMatch(t, []string{"@v/v1.2.3.zip", "@v/v1.2.3.mod", "@v/v1.2.3.info"}, goCleanupReferences(first))
	require.ElementsMatch(t, []string{"@v/v2.0.0.zip", "@v/v2.0.0.mod", "@v/v2.0.0.info"}, goCleanupReferences(second))
	require.Contains(t, first.Digests, "sha256:zip123")
	require.Contains(t, second.Digests, "sha256:zip200")
}

func TestGoCleanupFreshVersionSiblingRetainsOnlyThatVersion(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	inventory := goCleanupRecords(stale, map[string]string{
		"@v/v1.2.3.zip":  "sha256:zip123",
		"@v/v1.2.3.mod":  "sha256:mod123",
		"@v/v1.2.3.info": "sha256:info123",
	})
	markGoReferenceFresh(&inventory, "example.com/acme/lib", "@v/v1.2.3.info", fresh)
	inventory = mergeGoCleanupRecords(inventory, goCleanupRecords(stale, map[string]string{
		"@v/v2.0.0.zip": "sha256:zip200",
		"@v/v2.0.0.mod": "sha256:mod200",
	}))

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 2)
	freshGroup := goCleanupGroupForVersion(t, groups, "v1.2.3")
	require.True(t, freshGroup.LastAccessAt.Equal(fresh), "the fresh info file makes the complete Go version group recent")
	require.ElementsMatch(t, []string{"@v/v1.2.3.zip", "@v/v1.2.3.mod", "@v/v1.2.3.info"}, goCleanupReferences(freshGroup))
	otherGroup := goCleanupGroupForVersion(t, groups, "v2.0.0")
	require.True(t, otherGroup.LastAccessAt.Before(now.Add(-7*24*time.Hour)))
}

func TestGoCleanupGroupsSharedManifestDigestByVersionTags(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	digestValue := "sha256:identical-mod-content"
	inventory := ecosystem.CacheCleanupInventory{
		Manifests: []meta.ManifestRecord{{
			Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v2.0.0.mod",
			Digest: digestValue, AcceptKey: "go", CreatedAt: stale, UpdatedAt: stale,
		}},
		Tags: []meta.TagRecord{
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v1.2.3.mod", Digest: digestValue, CreatedAt: stale, UpdatedAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v2.0.0.mod", Digest: digestValue, CreatedAt: stale, UpdatedAt: stale},
		},
		RepoBlobs: []meta.RepoBlobRecord{{
			Alias: "proxy", Repository: "example.com/acme/lib", Digest: digestValue, CreatedAt: stale, UpdatedAt: stale, LastAccessAt: stale,
		}},
	}

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 2, "one manifest digest can have tag aliases in two distinct module versions")
	first := goCleanupGroupForVersion(t, groups, "v1.2.3")
	second := goCleanupGroupForVersion(t, groups, "v2.0.0")
	require.NotEqual(t, first.Package, second.Package)
	require.ElementsMatch(t, []string{"@v/v1.2.3.mod"}, goCleanupReferences(first))
	require.ElementsMatch(t, []string{"@v/v2.0.0.mod"}, goCleanupReferences(second))
}

func TestGoCleanupRecentSharedModOnlyRefreshesTheMatchingVersion(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	sharedDigest := "sha256:identical-mod-content"
	inventory := ecosystem.CacheCleanupInventory{
		Manifests: []meta.ManifestRecord{
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v1.2.3.zip", Digest: "sha256:zip123", AcceptKey: "go", CreatedAt: stale, UpdatedAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v1.2.3.info", Digest: "sha256:info123", AcceptKey: "go", CreatedAt: stale, UpdatedAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v2.0.0.zip", Digest: "sha256:zip200", AcceptKey: "go", CreatedAt: stale, UpdatedAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v2.0.0.info", Digest: "sha256:info200", AcceptKey: "go", CreatedAt: stale, UpdatedAt: stale},
			// The metadata key is digest-based, so the one shared manifest row retains a stale reference.
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v2.0.0.mod", Digest: sharedDigest, AcceptKey: "go", CreatedAt: stale, UpdatedAt: stale},
		},
		Tags: []meta.TagRecord{
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v1.2.3.zip", Digest: "sha256:zip123", CreatedAt: stale, UpdatedAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v1.2.3.info", Digest: "sha256:info123", CreatedAt: stale, UpdatedAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v1.2.3.mod", Digest: sharedDigest, CreatedAt: stale, UpdatedAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v2.0.0.zip", Digest: "sha256:zip200", CreatedAt: stale, UpdatedAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v2.0.0.info", Digest: "sha256:info200", CreatedAt: stale, UpdatedAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Reference: "@v/v2.0.0.mod", Digest: sharedDigest, CreatedAt: stale, UpdatedAt: fresh},
		},
		RepoBlobs: []meta.RepoBlobRecord{
			{Alias: "proxy", Repository: "example.com/acme/lib", Digest: "sha256:zip123", CreatedAt: stale, UpdatedAt: stale, LastAccessAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Digest: "sha256:info123", CreatedAt: stale, UpdatedAt: stale, LastAccessAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Digest: "sha256:zip200", CreatedAt: stale, UpdatedAt: stale, LastAccessAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Digest: "sha256:info200", CreatedAt: stale, UpdatedAt: stale, LastAccessAt: stale},
			{Alias: "proxy", Repository: "example.com/acme/lib", Digest: sharedDigest, CreatedAt: stale, UpdatedAt: fresh, LastAccessAt: fresh},
		},
		Blobs: []meta.BlobRecord{{Digest: sharedDigest, CreatedAt: stale, UpdatedAt: fresh, LastAccessAt: fresh}},
		Pulls: []meta.PullRecord{{
			Alias: "go/proxy", Repository: "example.com/acme/lib", Reference: "@v/v2.0.0.mod", CreatedAt: fresh, UpdatedAt: fresh, LastPullAt: fresh,
		}},
	}

	groups, err := (&runtimeAdapter{}).PlanCacheCleanup(context.Background(), inventory, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Len(t, groups, 2)
	oldVersion := goCleanupGroupForVersion(t, groups, "v1.2.3")
	newVersion := goCleanupGroupForVersion(t, groups, "v2.0.0")
	require.True(t, oldVersion.LastAccessAt.Before(now.Add(-7*24*time.Hour)), "a shared blob touch must not renew every version that references it")
	require.True(t, newVersion.LastAccessAt.Equal(fresh), "the newer version tag and scoped pull should refresh their own group")
	require.Contains(t, oldVersion.Digests, sharedDigest)
	require.Contains(t, newVersion.Digests, sharedDigest)
}

func goCleanupRecords(at time.Time, references map[string]string) ecosystem.CacheCleanupInventory {
	inventory := ecosystem.CacheCleanupInventory{}
	for reference, digest := range references {
		inventory.Manifests = append(inventory.Manifests, meta.ManifestRecord{
			Alias: "proxy", Repository: "example.com/acme/lib", Reference: reference, Digest: digest, AcceptKey: "go", CreatedAt: at, UpdatedAt: at,
		})
		inventory.Tags = append(inventory.Tags, meta.TagRecord{
			Alias: "proxy", Repository: "example.com/acme/lib", Reference: reference, Digest: digest, CreatedAt: at, UpdatedAt: at,
		})
		inventory.RepoBlobs = append(inventory.RepoBlobs, meta.RepoBlobRecord{
			Alias: "proxy", Repository: "example.com/acme/lib", Digest: digest, CreatedAt: at, UpdatedAt: at, LastAccessAt: at,
		})
	}
	return inventory
}

func mergeGoCleanupRecords(inventories ...ecosystem.CacheCleanupInventory) ecosystem.CacheCleanupInventory {
	merged := ecosystem.CacheCleanupInventory{}
	for i := range inventories {
		merged.Manifests = append(merged.Manifests, inventories[i].Manifests...)
		merged.Tags = append(merged.Tags, inventories[i].Tags...)
		merged.RepoBlobs = append(merged.RepoBlobs, inventories[i].RepoBlobs...)
	}
	return merged
}

func goCleanupGroupForVersion(t *testing.T, groups []ecosystem.CacheCleanupGroup, version string) ecosystem.CacheCleanupGroup {
	t.Helper()
	for i := range groups {
		for j := range groups[i].Tags {
			if strings.Contains(groups[i].Tags[j].Reference, version) {
				return groups[i]
			}
		}
	}
	t.Fatalf("cleanup plan missing Go version group %q", version)
	return ecosystem.CacheCleanupGroup{}
}

func goCleanupReferences(group ecosystem.CacheCleanupGroup) []string {
	references := make([]string, 0, len(group.Tags))
	for i := range group.Tags {
		references = append(references, group.Tags[i].Reference)
	}
	return references
}

func markGoReferenceFresh(inventory *ecosystem.CacheCleanupInventory, module, reference string, at time.Time) {
	digest := ""
	markGoTagFresh(inventory, module, reference, at, &digest)
	markGoManifestFresh(inventory, module, reference, at)
	markGoRepoBlobFresh(inventory, module, digest, at)
}

func markGoTagFresh(inventory *ecosystem.CacheCleanupInventory, module, reference string, at time.Time, digest *string) {
	for i := range inventory.Tags {
		if inventory.Tags[i].Repository == module && inventory.Tags[i].Reference == reference {
			inventory.Tags[i].UpdatedAt = at
			*digest = inventory.Tags[i].Digest
		}
	}
}

func markGoManifestFresh(inventory *ecosystem.CacheCleanupInventory, module, reference string, at time.Time) {
	for i := range inventory.Manifests {
		if inventory.Manifests[i].Repository == module && inventory.Manifests[i].Reference == reference {
			inventory.Manifests[i].UpdatedAt = at
		}
	}
}

func markGoRepoBlobFresh(inventory *ecosystem.CacheCleanupInventory, module, digest string, at time.Time) {
	for i := range inventory.RepoBlobs {
		if inventory.RepoBlobs[i].Repository == module && inventory.RepoBlobs[i].Digest == digest {
			inventory.RepoBlobs[i].LastAccessAt = at
		}
	}
}
