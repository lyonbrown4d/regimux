package pypi

import (
	"context"
	"fmt"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (r *runtimeAdapter) PlanCacheCleanup(ctx context.Context, inventory ecosystem.CacheCleanupInventory, opts ecosystem.CacheCleanupOptions) ([]ecosystem.CacheCleanupGroup, error) {
	groups, err := artifactcache.PlanCacheCleanup(ctx, ecosystemPyPI, inventory, opts, pypiCleanupGroupKey)
	if err != nil {
		return nil, fmt.Errorf("plan pypi cleanup: %w", err)
	}
	return groups, nil
}

func pypiCleanupGroupKey(repository, reference string) string {
	if strings.HasPrefix(repository, "pypi/simple/") {
		return repository + "\x00" + reference
	}
	if repository == "pypi/packages" {
		artifact, query, hasQuery := strings.Cut(reference, "?")
		group := repository + "\x00" + stripPyPIArtifactSidecars(artifact)
		if hasQuery {
			group += "?" + query
		}
		return group
	}
	return repository + "\x00" + reference
}

func stripPyPIArtifactSidecars(reference string) string {
	for {
		stripped, changed := stripSuffixFrom(reference, ".metadata", ".sha256", ".sha512", ".sha1", ".md5", ".asc", ".sig")
		if !changed {
			return reference
		}
		reference = stripped
	}
}

func stripSuffixFrom(value string, suffixes ...string) (string, bool) {
	for _, suffix := range suffixes {
		if stripped, ok := strings.CutSuffix(value, suffix); ok {
			return stripped, true
		}
	}
	return value, false
}

var _ ecosystem.CacheCleanupPlanner = (*runtimeAdapter)(nil)
