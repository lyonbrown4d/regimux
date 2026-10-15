package npm

import (
	"context"
	"fmt"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (r *runtimeAdapter) PlanCacheCleanup(ctx context.Context, inventory ecosystem.CacheCleanupInventory, opts ecosystem.CacheCleanupOptions) ([]ecosystem.CacheCleanupGroup, error) {
	groups, err := artifactcache.PlanCacheCleanup(ctx, ecosystemNPM, inventory, opts, npmCleanupGroupKey)
	if err != nil {
		return nil, fmt.Errorf("plan npm cleanup: %w", err)
	}
	return groups, nil
}

func npmCleanupGroupKey(repository, reference string) string {
	if reference == metadataRef {
		return repository + "\x00catalog"
	}
	if tarball, ok := strings.CutPrefix(reference, "tarball:"); ok {
		return repository + "\x00tarball\x00" + stripNPMSidecars(tarball)
	}
	return repository + "\x00" + reference
}

func stripNPMSidecars(reference string) string {
	for {
		changed := false
		for _, suffix := range []string{".sha256", ".sha512", ".sha1", ".md5", ".asc", ".sig"} {
			if stripped, ok := strings.CutSuffix(reference, suffix); ok {
				reference = stripped
				changed = true
				break
			}
		}
		if !changed {
			return reference
		}
	}
}

var _ ecosystem.CacheCleanupPlanner = (*runtimeAdapter)(nil)
