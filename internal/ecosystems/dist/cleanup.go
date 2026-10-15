package dist

import (
	"context"
	"fmt"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (r *runtimeAdapter) PlanCacheCleanup(ctx context.Context, inventory ecosystem.CacheCleanupInventory, opts ecosystem.CacheCleanupOptions) ([]ecosystem.CacheCleanupGroup, error) {
	groups, err := artifactcache.PlanCacheCleanup(ctx, "dist", inventory, opts, distCleanupGroupKey)
	if err != nil {
		return nil, fmt.Errorf("plan dist cleanup: %w", err)
	}
	return groups, nil
}

func distCleanupGroupKey(repository, reference string) string {
	return repository + "\x00" + stripDistSidecars(reference)
}

func stripDistSidecars(reference string) string {
	for {
		stripped := reference
		changed := false
		for _, suffix := range []string{".sha256", ".sha512", ".sha1", ".md5", ".asc", ".sig"} {
			if candidate, ok := strings.CutSuffix(reference, suffix); ok {
				stripped, changed = candidate, true
				break
			}
		}
		if !changed {
			return reference
		}
		reference = stripped
	}
}

var _ ecosystem.CacheCleanupPlanner = (*runtimeAdapter)(nil)
