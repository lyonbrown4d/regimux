package artifactcache_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/stretchr/testify/require"
)

func TestPlanCacheCleanupGroupsTagsAndCountsSharedActivity(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	inventory := ecosystem.CacheCleanupInventory{
		Manifests: []meta.ManifestRecord{{Alias: "mirror", Repository: "example.com/lib", Reference: "@v/v2.0.0.mod", AcceptKey: ecosystem.Go, Digest: "sha256:shared", UpdatedAt: old}},
		Tags: []meta.TagRecord{
			{Alias: "mirror", Repository: "example.com/lib", Reference: "@v/v1.0.0.mod", Digest: "sha256:shared", UpdatedAt: old},
			{Alias: "mirror", Repository: "example.com/lib", Reference: "@v/v2.0.0.mod", Digest: "sha256:shared", UpdatedAt: old},
		},
		Blobs: []meta.BlobRecord{{Digest: "sha256:shared", LastAccessAt: fresh}},
		Pulls: []meta.PullRecord{{Alias: "go/mirror", Repository: "example.com/lib", Reference: "@v/v1.0.0.mod", LastPullAt: fresh}},
	}
	groups, err := artifactcache.PlanCacheCleanup(context.Background(), ecosystem.Go, inventory, ecosystem.CacheCleanupOptions{Now: now, UnusedFor: time.Hour}, func(repo, reference string) string {
		return repo + "@" + reference
	})
	require.NoError(t, err)
	require.Len(t, groups, 2)
	for _, group := range groups {
		require.Equal(t, []string{"sha256:shared"}, group.Digests)
		if strings.HasSuffix(group.Package, "@v/v1.0.0.mod") {
			require.Equal(t, fresh, group.LastAccessAt)
		} else {
			require.Equal(t, old, group.LastAccessAt, "a shared blob access must not keep every version active")
		}
	}
}
