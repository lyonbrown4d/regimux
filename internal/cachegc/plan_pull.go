package cachegc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) packageHasRecentPull(ctx context.Context, recentPulls map[meta.PullKey]struct{}, group *ecosystem.CacheCleanupGroup, cutoff time.Time) (bool, error) {
	checked := make(map[meta.PullKey]struct{}, len(group.Tags)+len(group.Manifests))
	for i := range group.Tags {
		tag := &group.Tags[i]
		key := meta.PullKey{Alias: tag.Alias, Repository: tag.Repository, Reference: tag.Reference}
		if recent, err := s.packagePullIsRecent(ctx, recentPulls, checked, group.Ecosystem, key, cutoff); err != nil || recent {
			return recent, err
		}
	}
	for i := range group.Manifests {
		if recent, err := s.packageManifestHasRecentPull(ctx, recentPulls, checked, group.Ecosystem, &group.Manifests[i], cutoff); err != nil || recent {
			return recent, err
		}
	}
	return false, nil
}

func (s *Service) packageManifestHasRecentPull(
	ctx context.Context,
	recentPulls, checked map[meta.PullKey]struct{},
	ecosystemName string,
	manifest *meta.ManifestRecord,
	cutoff time.Time,
) (bool, error) {
	for _, reference := range [...]string{manifest.Reference, manifest.Digest} {
		key := meta.PullKey{Alias: manifest.Alias, Repository: manifest.Repository, Reference: reference}
		if recent, err := s.packagePullIsRecent(ctx, recentPulls, checked, ecosystemName, key, cutoff); err != nil || recent {
			return recent, err
		}
	}
	return false, nil
}

func (s *Service) packagePullIsRecent(
	ctx context.Context,
	recentPulls map[meta.PullKey]struct{},
	checked map[meta.PullKey]struct{},
	ecosystemName string,
	key meta.PullKey,
	cutoff time.Time,
) (bool, error) {
	if key.Repository == "" || key.Reference == "" {
		return false, nil
	}
	key.Alias = cleanupPullAlias(ecosystemName, key.Alias)
	if _, ok := checked[key]; ok {
		return false, nil
	}
	checked[key] = struct{}{}
	if _, ok := recentPulls[key]; ok {
		return true, nil
	}
	pull, found, err := s.metadata.Pull(ctx, key)
	if err != nil {
		return false, fmt.Errorf("recheck package pull: %w", err)
	}
	recent := found && pull != nil && !latestPullActivity(*pull).Before(cutoff)
	if recent {
		recentPulls[key] = struct{}{}
	}
	return recent, nil
}

func latestPullActivity(pull meta.PullRecord) time.Time {
	latest := pull.LastPullAt
	for _, at := range [...]time.Time{pull.LastUpstreamPullAt, pull.UpdatedAt, pull.CreatedAt} {
		if at.After(latest) {
			latest = at
		}
	}
	return latest
}

func cleanupPullAlias(ecosystemName, alias string) string {
	if ecosystemName == ecosystem.Dist || ecosystemName == ecosystem.Container || strings.TrimSpace(alias) == "" {
		return alias
	}
	prefix := ecosystemName + "/"
	if strings.HasPrefix(alias, prefix) {
		return alias
	}
	return ecosystem.ScopedAlias(ecosystemName, alias)
}
