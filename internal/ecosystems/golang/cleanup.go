package golang

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (r *runtimeAdapter) PlanCacheCleanup(ctx context.Context, inventory ecosystem.CacheCleanupInventory, opts ecosystem.CacheCleanupOptions) ([]ecosystem.CacheCleanupGroup, error) {
	groups, err := artifactcache.PlanCacheCleanup(ctx, ecosystem.Go, inventory, opts, goCleanupPackageKey)
	if err != nil {
		return nil, fmt.Errorf("plan Go cleanup: %w", err)
	}
	return groups, nil
}

func goCleanupPackageKey(repository, reference string) string {
	if strings.HasPrefix(reference, "@v/") {
		version := path.Base(reference)
		for _, suffix := range []string{".info", ".mod", ".zip"} {
			version = strings.TrimSuffix(version, suffix)
		}
		if version != "" {
			return repository + "@" + version
		}
	}
	return repository + "@meta:" + reference
}

var _ ecosystem.CacheCleanupPlanner = (*runtimeAdapter)(nil)
