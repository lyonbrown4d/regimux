package cachegc

import (
	"context"
	"fmt"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

type Option func(*Service)

func WithPackageInvalidator(invalidate func(context.Context, ecosystem.CacheCleanupGroup) error) Option {
	return func(service *Service) { service.invalidatePackage = invalidate }
}

func (s *Service) invalidatePackageCaches(ctx context.Context, groups []ecosystem.CacheCleanupGroup, selected []bool, retired packageRetirement, candidates map[string]bool) error {
	if s.invalidatePackage == nil {
		return nil
	}
	if err := s.invalidateSelectedPackageCaches(ctx, groups, selected, retired); err != nil {
		return err
	}
	return s.invalidateOrphanCaches(ctx, groups, selected, retired, candidates)
}

func (s *Service) invalidateSelectedPackageCaches(ctx context.Context, groups []ecosystem.CacheCleanupGroup, selected []bool, retired packageRetirement) error {
	for i := range groups {
		if !selected[i] {
			continue
		}
		group := invalidationGroup(groups[i], retired)
		if emptyInvalidationGroup(group) {
			continue
		}
		if err := s.invalidatePackage(ctx, group); err != nil {
			return fmt.Errorf("invalidate %s package cache: %w", group.Ecosystem, err)
		}
	}
	return nil
}

func emptyInvalidationGroup(group ecosystem.CacheCleanupGroup) bool {
	return len(group.Tags)+len(group.Manifests)+len(group.RepoBlobs) == 0
}

func (s *Service) invalidateOrphanCaches(ctx context.Context, groups []ecosystem.CacheCleanupGroup, selected []bool, retired packageRetirement, candidates map[string]bool) error {
	orphans := orphanInvalidationGroup(groups, selected, retired, candidates)
	if len(orphans.RepoBlobs)+len(orphans.Digests) > 0 {
		if err := s.invalidatePackage(ctx, orphans); err != nil {
			return fmt.Errorf("invalidate orphan cache: %w", err)
		}
	}
	return nil
}

func orphanInvalidationGroup(groups []ecosystem.CacheCleanupGroup, selected []bool, retired packageRetirement, candidates map[string]bool) ecosystem.CacheCleanupGroup {
	coveredLinks, ownedDigests := orphanCoverage(groups, selected)
	orphans := ecosystem.CacheCleanupGroup{Package: "orphan"}
	appendOrphanLinks(&orphans, retired.links, coveredLinks)
	appendOrphanDigests(&orphans, candidates, ownedDigests)
	return orphans
}

func orphanCoverage(groups []ecosystem.CacheCleanupGroup, selected []bool) (map[string]bool, map[string]bool) {
	coveredLinks := make(map[string]bool)
	ownedDigests := make(map[string]bool)
	for i := range groups {
		for _, digest := range groups[i].Digests {
			ownedDigests[digest] = true
		}
		if selected[i] {
			for j := range groups[i].RepoBlobs {
				link := &groups[i].RepoBlobs[j]
				coveredLinks[cleanupLinkKey(*link)] = true
			}
		}
	}
	return coveredLinks, ownedDigests
}

func appendOrphanLinks(orphans *ecosystem.CacheCleanupGroup, links map[string]meta.RepoBlobRecord, covered map[string]bool) {
	for key := range links {
		if !covered[key] {
			orphans.RepoBlobs = append(orphans.RepoBlobs, links[key])
		}
	}
}

func appendOrphanDigests(orphans *ecosystem.CacheCleanupGroup, candidates, owned map[string]bool) {
	for digest := range candidates {
		if !owned[digest] {
			orphans.Digests = append(orphans.Digests, digest)
		}
	}
}

func invalidationGroup(source ecosystem.CacheCleanupGroup, retired packageRetirement) ecosystem.CacheCleanupGroup {
	group := ecosystem.CacheCleanupGroup{
		Ecosystem: source.Ecosystem, Alias: source.Alias, Package: source.Package, LastAccessAt: source.LastAccessAt,
	}
	for i := range source.Tags {
		record := &source.Tags[i]
		if _, removing := retired.tags[cleanupTagKey(*record)]; removing {
			group.Tags = append(group.Tags, *record)
		}
	}
	for i := range source.Manifests {
		record := &source.Manifests[i]
		if _, removing := retired.manifests[cleanupManifestKey(*record)]; removing {
			group.Manifests = append(group.Manifests, *record)
		}
	}
	for i := range source.RepoBlobs {
		record := &source.RepoBlobs[i]
		if _, removing := retired.links[cleanupLinkKey(*record)]; removing {
			group.RepoBlobs = append(group.RepoBlobs, *record)
		}
	}
	return group
}
