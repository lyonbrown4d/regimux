package container

import (
	"context"
	"fmt"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (r *Runtime) planUntaggedManifests(
	ctx context.Context,
	inventory ecosystem.CacheCleanupInventory,
	index cleanupIndex,
	ownedManifestKeys map[string]struct{},
) ([]ecosystem.CacheCleanupGroup, error) {
	childDigests, childErr := r.untaggedChildDigests(ctx, inventory.Manifests, ownedManifestKeys)
	if childErr != nil {
		return nil, childErr
	}

	groups := make([]ecosystem.CacheCleanupGroup, 0, len(inventory.Manifests))
	for i := range inventory.Manifests {
		if err := planContextErr(ctx); err != nil {
			return nil, err
		}
		manifest := inventory.Manifests[i]
		if !isStandaloneRoot(manifest, ownedManifestKeys, childDigests) {
			continue
		}
		group, err := r.planStandaloneImage(ctx, manifest, index)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
		for j := range group.Manifests {
			ownedManifestKeys[cleanupManifestKey(group.Manifests[j])] = struct{}{}
		}
	}
	return groups, nil
}

func (r *Runtime) untaggedChildDigests(ctx context.Context, manifests []meta.ManifestRecord, owned map[string]struct{}) (map[string]struct{}, error) {
	children := make(map[string]struct{})
	for i := range manifests {
		manifest := manifests[i]
		if !isContainerManifest(manifest) || containsManifest(owned, manifest) {
			continue
		}
		references, err := r.cleanup.OCIReferences(ctx, manifest)
		if err != nil {
			return nil, fmt.Errorf("index untagged OCI graph references for %s: %w", manifest.Digest, err)
		}
		for _, digest := range references {
			children[digest] = struct{}{}
		}
	}
	return children, nil
}

func isStandaloneRoot(manifest meta.ManifestRecord, owned, children map[string]struct{}) bool {
	_, isChild := children[manifest.Digest]
	return isContainerManifest(manifest) && !containsManifest(owned, manifest) && !isChild
}

func (r *Runtime) planStandaloneImage(
	ctx context.Context,
	manifest meta.ManifestRecord,
	index cleanupIndex,
) (ecosystem.CacheCleanupGroup, error) {
	group := ecosystem.CacheCleanupGroup{
		Ecosystem: ecosystem.Container,
		Alias:     manifest.Alias,
		Package:   manifest.Repository + "@" + manifest.Digest,
	}
	seen := make(map[string]struct{})
	if err := r.addImageGraph(ctx, manifest.Digest, manifest.Alias, manifest.Repository, index, seen, &group); err != nil {
		return ecosystem.CacheCleanupGroup{}, fmt.Errorf("plan untagged OCI image %s: %w", manifest.Digest, err)
	}
	group.LastAccessAt = standalonePullActivity(group.LastAccessAt, manifest, index.pullsByReference)
	group.RepoBlobs = repoBlobsForImage(index.allRepoBlobs, manifest.Alias, manifest.Repository, seen)
	for i := range group.RepoBlobs {
		row := group.RepoBlobs[i]
		if row.Digest == manifest.Digest {
			group.LastAccessAt = latest(group.LastAccessAt, row.LastAccessAt, row.LastVerifiedAt, row.UpdatedAt, row.CreatedAt)
		}
	}
	return group, nil
}

func cleanupManifestKey(manifest meta.ManifestRecord) string {
	return (meta.ManifestKey{Alias: manifest.Alias, Repository: manifest.Repository, Digest: manifest.Digest}).String()
}

func containsManifest(keys map[string]struct{}, manifest meta.ManifestRecord) bool {
	_, found := keys[cleanupManifestKey(manifest)]
	return found
}

func standalonePullActivity(current time.Time, manifest meta.ManifestRecord, pulls map[string][]meta.PullRecord) time.Time {
	for _, reference := range []string{manifest.Reference, manifest.Digest} {
		rows := pulls[refKey(manifest.Alias, manifest.Repository, reference)]
		for i := range rows {
			row := rows[i]
			current = latest(current, row.LastPullAt, row.UpdatedAt, row.CreatedAt)
		}
	}
	return current
}
