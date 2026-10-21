package cachegc

import (
	"context"
	"errors"
	"fmt"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

type packageRetirement struct {
	tags      map[string]meta.TagRecord
	manifests map[string]meta.ManifestRecord
	links     map[string]meta.RepoBlobRecord
}

func packageRetirements(inventory ecosystem.CacheCleanupInventory, groups []ecosystem.CacheCleanupGroup, selected []bool) packageRetirement {
	retired, kept := newPackageRetirement(), newPackageRetirement()
	for i := range groups {
		target := &kept
		if selected[i] {
			target = &retired
		}
		target.include(&groups[i])
	}
	for key := range kept.tags {
		delete(retired.tags, key)
	}
	// A tag retained by any package keeps its manifest even when the bytes are
	// identical to an artifact selected in a different logical package.
	for i := range inventory.Tags {
		tag := &inventory.Tags[i]
		if _, removed := retired.tags[cleanupTagKey(*tag)]; !removed {
			key := (meta.ManifestKey{Alias: tag.Alias, Repository: tag.Repository, Digest: tag.Digest}).String()
			kept.manifests[key] = meta.ManifestRecord{}
		}
	}
	for key := range kept.manifests {
		delete(retired.manifests, key)
	}
	for key := range kept.links {
		delete(retired.links, key)
	}
	return retired
}

func newPackageRetirement() packageRetirement {
	return packageRetirement{
		tags: make(map[string]meta.TagRecord), manifests: make(map[string]meta.ManifestRecord), links: make(map[string]meta.RepoBlobRecord),
	}
}

func (r *packageRetirement) include(group *ecosystem.CacheCleanupGroup) {
	for i := range group.Tags {
		tag := &group.Tags[i]
		r.tags[cleanupTagKey(*tag)] = *tag
	}
	for i := range group.Manifests {
		manifest := &group.Manifests[i]
		r.manifests[cleanupManifestKey(*manifest)] = *manifest
	}
	for i := range group.RepoBlobs {
		link := &group.RepoBlobs[i]
		r.links[cleanupLinkKey(*link)] = *link
	}
}

func retireOrphanLinks(inventory ecosystem.CacheCleanupInventory, opts ecosystem.CacheCleanupOptions, candidates map[string]bool, retired *packageRetirement) {
	keptRepos := make(map[string]bool)
	for i := range inventory.Manifests {
		manifest := &inventory.Manifests[i]
		if _, removed := retired.manifests[cleanupManifestKey(*manifest)]; !removed {
			keptRepos[manifest.Alias+"\x00"+manifest.Repository] = true
		}
	}
	for i := range inventory.RepoBlobs {
		link := &inventory.RepoBlobs[i]
		repo := link.Alias + "\x00" + link.Repository
		if !candidates[link.Digest] || keptRepos[repo] {
			continue
		}
		if link.LastAccessAt.IsZero() || isRecent(opts.Now.Add(-opts.UnusedFor), link.LastAccessAt, link.LastVerifiedAt, link.UpdatedAt, link.CreatedAt) {
			continue
		}
		retired.links[cleanupLinkKey(*link)] = *link
	}
}

func (s *Service) retirePackageMetadata(ctx context.Context, retired packageRetirement) error {
	for key := range retired.tags {
		tag := retired.tags[key]
		deleted, err := deleteTagIfUnchanged(ctx, s.metadata, tag)
		if deleteErr := checkPackageDelete(deleted, err, "tag"); deleteErr != nil {
			return deleteErr
		}
	}
	for key := range retired.manifests {
		manifest := retired.manifests[key]
		deleted, err := deleteManifestIfUnchanged(ctx, s.metadata, manifest)
		if deleteErr := checkPackageDelete(deleted, err, "manifest"); deleteErr != nil {
			return deleteErr
		}
	}
	for key := range retired.links {
		link := retired.links[key]
		deleted, err := deleteRepoBlobIfUnchanged(ctx, s.metadata, link)
		if deleteErr := checkPackageDelete(deleted, err, "repository link"); deleteErr != nil {
			return deleteErr
		}
	}
	return nil
}

func checkPackageDelete(deleted bool, err error, kind string) error {
	if err != nil {
		return fmt.Errorf("retire package %s: %w", kind, err)
	}
	if !deleted {
		return errors.New("package metadata changed during cleanup; object reclamation deferred")
	}
	return nil
}

func cleanupTagKey(tag meta.TagRecord) string {
	return (meta.TagKey{Alias: tag.Alias, Repository: tag.Repository, Reference: tag.Reference}).String()
}

func cleanupManifestKey(manifest meta.ManifestRecord) string {
	return (meta.ManifestKey{Alias: manifest.Alias, Repository: manifest.Repository, Digest: manifest.Digest}).String()
}

func cleanupLinkKey(link meta.RepoBlobRecord) string {
	return (meta.RepoBlobKey{Alias: link.Alias, Repository: link.Repository, Digest: link.Digest}).String()
}

// The dry-run view simulates reference retirement without any metadata writes.
// The ordinary CAS collector can then compute the same physical deletion plan.
type cleanupMetadataView struct {
	meta.Store
	inventory ecosystem.CacheCleanupInventory
	retired   packageRetirement
}

func (v *cleanupMetadataView) ReadCleanupSnapshot(ctx context.Context) (meta.CleanupSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return meta.CleanupSnapshot{}, fmt.Errorf("read dry-run cleanup snapshot: %w", err)
	}
	snapshot := meta.CleanupSnapshot{
		Blobs: append([]meta.BlobRecord(nil), v.inventory.Blobs...),
		Pulls: append([]meta.PullRecord(nil), v.inventory.Pulls...),
	}
	var err error
	if snapshot.Manifests, err = remainingManifests(ctx, v); err != nil {
		return meta.CleanupSnapshot{}, err
	}
	if snapshot.Tags, err = remainingTags(ctx, v); err != nil {
		return meta.CleanupSnapshot{}, err
	}
	if snapshot.RepoBlobs, err = remainingRepoBlobs(ctx, v); err != nil {
		return meta.CleanupSnapshot{}, err
	}
	return snapshot, nil
}

func remainingManifests(ctx context.Context, view *cleanupMetadataView) ([]meta.ManifestRecord, error) {
	var result []meta.ManifestRecord
	for i := range view.inventory.Manifests {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("read dry-run manifests: %w", err)
		}
		manifest := view.inventory.Manifests[i]
		if _, removed := view.retired.manifests[cleanupManifestKey(manifest)]; !removed {
			result = append(result, manifest)
		}
	}
	return result, nil
}

func remainingTags(ctx context.Context, view *cleanupMetadataView) ([]meta.TagRecord, error) {
	var result []meta.TagRecord
	for i := range view.inventory.Tags {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("read dry-run tags: %w", err)
		}
		tag := view.inventory.Tags[i]
		if _, removed := view.retired.tags[cleanupTagKey(tag)]; !removed {
			result = append(result, tag)
		}
	}
	return result, nil
}

func remainingRepoBlobs(ctx context.Context, view *cleanupMetadataView) ([]meta.RepoBlobRecord, error) {
	var result []meta.RepoBlobRecord
	for i := range view.inventory.RepoBlobs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("read dry-run repository links: %w", err)
		}
		link := view.inventory.RepoBlobs[i]
		if _, removed := view.retired.links[cleanupLinkKey(link)]; !removed {
			result = append(result, link)
		}
	}
	return result, nil
}

func (v *cleanupMetadataView) ListManifests(ctx context.Context) (*collectionlist.List[meta.ManifestRecord], error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read dry-run manifests: %w", err)
	}
	result := collectionlist.NewList[meta.ManifestRecord]()
	for i := range v.inventory.Manifests {
		manifest := &v.inventory.Manifests[i]
		if _, removed := v.retired.manifests[cleanupManifestKey(*manifest)]; !removed {
			result.Add(*manifest)
		}
	}
	return result, nil
}

func (v *cleanupMetadataView) ListTags(ctx context.Context) (*collectionlist.List[meta.TagRecord], error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read dry-run tags: %w", err)
	}
	result := collectionlist.NewList[meta.TagRecord]()
	for i := range v.inventory.Tags {
		tag := &v.inventory.Tags[i]
		if _, removed := v.retired.tags[cleanupTagKey(*tag)]; !removed {
			result.Add(*tag)
		}
	}
	return result, nil
}

func (v *cleanupMetadataView) ListRepoBlobs(ctx context.Context, opts ...meta.RepoBlobListOption) (*collectionlist.List[meta.RepoBlobRecord], error) {
	// Preserve option handling for callers other than the global collector.
	if len(opts) > 0 {
		list, err := v.Store.ListRepoBlobs(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("list repository blobs for cleanup metadata view: %w", err)
		}
		return list, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read dry-run repository links: %w", err)
	}
	result := collectionlist.NewList[meta.RepoBlobRecord]()
	for i := range v.inventory.RepoBlobs {
		link := &v.inventory.RepoBlobs[i]
		if _, removed := v.retired.links[cleanupLinkKey(*link)]; !removed {
			result.Add(*link)
		}
	}
	return result, nil
}
