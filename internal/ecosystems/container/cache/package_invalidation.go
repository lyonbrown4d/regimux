package cache

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

// InvalidatePackageCache removes transient OCI cache entries for the exact
// references and repository blobs selected for retirement by the cleanup plan.
// The tags/list cache is independent and remains governed by its normal TTL.
func (p manifestProxy) InvalidatePackageCache(ctx context.Context, group ecosystem.CacheCleanupGroup) error {
	if group.Ecosystem != ecosystem.Container {
		return nil
	}
	if err := validatePackageInvalidationContext(ctx); err != nil {
		return err
	}
	if p.cache == nil {
		return nil
	}
	if err := p.invalidateManifestPrefixes(ctx, group); err != nil {
		return err
	}
	return p.invalidateSmallBlobEntries(ctx, group)
}

func validatePackageInvalidationContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("invalidate OCI package cache: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("invalidate OCI package cache: %w", err)
	}
	return nil
}

func (p manifestProxy) invalidateManifestPrefixes(ctx context.Context, group ecosystem.CacheCleanupGroup) error {
	prefixes := manifestCachePrefixes(group)
	if len(prefixes) == 0 {
		return nil
	}
	deleter, ok := p.cache.(backend.PrefixDeleter)
	if !ok {
		return errors.New("invalidate OCI package cache: cache backend does not support prefix deletion")
	}
	for _, prefix := range prefixes {
		if err := deleter.DeletePrefix(ctx, prefix); err != nil {
			return fmt.Errorf("invalidate OCI manifest cache prefix: %w", err)
		}
	}
	return nil
}

func (p manifestProxy) invalidateSmallBlobEntries(ctx context.Context, group ecosystem.CacheCleanupGroup) error {
	if !p.smallCache.enabled {
		return nil
	}
	for _, digest := range uniqueRepoBlobDigests(group) {
		if err := p.cache.Delete(ctx, smallBlobCacheKey(digest)); err != nil {
			return fmt.Errorf("invalidate OCI small blob cache for %s: %w", digest, err)
		}
	}
	return nil
}

func manifestCachePrefixes(group ecosystem.CacheCleanupGroup) []string {
	prefixSet := make(map[string]struct{}, len(group.Tags)+2*len(group.Manifests))
	for i := range group.Tags {
		tag := &group.Tags[i]
		addManifestCachePrefix(prefixSet, tag.Alias, tag.Repository, tag.Reference)
	}
	for i := range group.Manifests {
		manifest := &group.Manifests[i]
		addManifestCachePrefix(prefixSet, manifest.Alias, manifest.Repository, manifest.Reference)
		addManifestCachePrefix(prefixSet, manifest.Alias, manifest.Repository, manifest.Digest)
	}
	prefixes := make([]string, 0, len(prefixSet))
	for prefix := range prefixSet {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	return prefixes
}

func addManifestCachePrefix(prefixes map[string]struct{}, alias, repository, reference string) {
	if alias == "" || repository == "" || reference == "" {
		return
	}
	prefix := strings.Join([]string{"manifest", alias, repository, reference}, ":") + ":"
	prefixes[prefix] = struct{}{}
}

func uniqueRepoBlobDigests(group ecosystem.CacheCleanupGroup) []string {
	digestSet := make(map[string]struct{}, len(group.RepoBlobs)+len(group.Digests))
	for i := range group.RepoBlobs {
		if digest := group.RepoBlobs[i].Digest; digest != "" {
			digestSet[digest] = struct{}{}
		}
	}
	for _, digest := range group.Digests {
		if digest != "" {
			digestSet[digest] = struct{}{}
		}
	}
	digests := make([]string, 0, len(digestSet))
	for digest := range digestSet {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	return digests
}

var _ PackageCacheInvalidator = manifestProxy{}
