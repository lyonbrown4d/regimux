package maven

import (
	"context"
	"fmt"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (r *runtimeAdapter) PlanCacheCleanup(ctx context.Context, inventory ecosystem.CacheCleanupInventory, opts ecosystem.CacheCleanupOptions) ([]ecosystem.CacheCleanupGroup, error) {
	groups, err := artifactcache.PlanCacheCleanup(ctx, ecosystem.Maven, inventory, opts, func(repository, _ string) string {
		return repository
	})
	if err != nil {
		return nil, fmt.Errorf("plan Maven cleanup: %w", err)
	}
	return groups, nil
}

var _ ecosystem.CacheCleanupPlanner = (*runtimeAdapter)(nil)
