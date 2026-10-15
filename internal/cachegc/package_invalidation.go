package cachegc

import (
	"context"
	"fmt"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

type Option func(*Service)

func WithPackageInvalidator(invalidate func(context.Context, ecosystem.CacheCleanupGroup) error) Option {
	return func(service *Service) { service.invalidatePackage = invalidate }
}

func (s *Service) invalidatePackageCaches(ctx context.Context, groups []ecosystem.CacheCleanupGroup, selected []bool, retired packageRetirement, candidates map[string]bool) error {
	if s.invalidatePackage == nil {
		return nil
	}
	for i := range groups {
		if !selected[i] {
			continue
		}
		group := invalidationGroup(groups[i], retired)
		if len(group.Tags)+len(group.Manifests)+len(group.RepoBlobs) == 0 {
			continue
		}
		if err := s.invalidatePackage(ctx, group); err != nil {
			return fmt.Errorf("invalidate %s package cache: %w", group.Ecosystem, err)
		}
	}
	orphans := orphanInvalidationGroup(groups, selected, retired, candidates)
	if len(orphans.RepoBlobs)+len(orphans.Digests) > 0 {
		if err := s.invalidatePackage(ctx, orphans); err != nil {
			return fmt.Errorf("invalidate orphan cache: %w", err)
		}
	}
	return nil
}

func orphanInvalidationGroup(groups []ecosystem.CacheCleanupGroup, selected []bool, retired packageRetirement, candidates map[string]bool) ecosystem.CacheCleanupGroup {
	coveredLinks := make(map[string]bool)
	ownedDigests := make(map[string]bool)
	for i := range groups {
		for _, digest := range groups[i].Digests {
			ownedDigests[digest] = true
		}
		if selected[i] {
			for _, link := range groups[i].RepoBlobs {
				coveredLinks[cleanupLinkKey(link)] = true
			}
		}
	}
	orphans := ecosystem.CacheCleanupGroup{Package: "orphan"}
	for key, link := range retired.links {
		if !coveredLinks[key] {
			orphans.RepoBlobs = append(orphans.RepoBlobs, link)
		}
	}
	for digest := range candidates {
		if !ownedDigests[digest] {
			orphans.Digests = append(orphans.Digests, digest)
		}
	}
	return orphans
}

func invalidationGroup(source ecosystem.CacheCleanupGroup, retired packageRetirement) ecosystem.CacheCleanupGroup {
	group := ecosystem.CacheCleanupGroup{
		Ecosystem: source.Ecosystem, Alias: source.Alias, Package: source.Package, LastAccessAt: source.LastAccessAt,
	}
	for _, record := range source.Tags {
		if _, removing := retired.tags[cleanupTagKey(record)]; removing {
			group.Tags = append(group.Tags, record)
		}
	}
	for _, record := range source.Manifests {
		if _, removing := retired.manifests[cleanupManifestKey(record)]; removing {
			group.Manifests = append(group.Manifests, record)
		}
	}
	for _, record := range source.RepoBlobs {
		if _, removing := retired.links[cleanupLinkKey(record)]; removing {
			group.RepoBlobs = append(group.RepoBlobs, record)
		}
	}
	return group
}
