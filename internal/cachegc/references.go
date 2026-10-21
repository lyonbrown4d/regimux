package cachegc

import (
	"context"
	"fmt"
	"time"

	"github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

type referenceSnapshot struct {
	protected      *set.Set[string]
	staleTags      []meta.TagRecord
	staleManifests []meta.ManifestRecord
	staleRepoBlobs []meta.RepoBlobRecord
}

func (s *Service) loadReferences(ctx context.Context, cutoff time.Time, prune bool) (*referenceSnapshot, error) {
	snapshot, err := meta.ReadCleanupSnapshot(ctx, s.metadata)
	if err != nil {
		return nil, fmt.Errorf("read reference metadata for cleanup: %w", err)
	}
	return s.loadReferencesFromSnapshot(ctx, cutoff, prune, snapshot)
}

func (s *Service) loadReferencesFromSnapshot(ctx context.Context, cutoff time.Time, prune bool, snapshot meta.CleanupSnapshot) (*referenceSnapshot, error) {
	out := &referenceSnapshot{protected: set.NewSet[string]()}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read reference metadata for cleanup: %w", err)
	}
	if !prune {
		return s.protectAllManifestReferences(ctx, snapshot.Manifests, out.protected)
	}
	if err := protectRecentBlobActivity(ctx, cutoff, snapshot.Blobs, out.protected); err != nil {
		return nil, err
	}
	activePulls, err := activePullReferences(ctx, cutoff, snapshot.Pulls)
	if err != nil {
		return nil, err
	}
	activeTags, activeManifestKeys, err := activeTagReferences(ctx, cutoff, snapshot.Tags, activePulls)
	if err != nil {
		return nil, err
	}
	if err := classifyManifestReferences(ctx, cutoff, snapshot.Manifests, activeManifestKeys, out); err != nil {
		return nil, err
	}
	if err := classifyTagReferences(ctx, snapshot.Tags, activeTags, out); err != nil {
		return nil, err
	}
	if err := classifyRepoBlobReferences(ctx, cutoff, snapshot.RepoBlobs, out); err != nil {
		return nil, err
	}
	if err := s.protectManifestGraph(ctx, snapshot.Manifests, out.protected, true); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) protectAllManifestReferences(ctx context.Context, manifests []meta.ManifestRecord, protected *set.Set[string]) (*referenceSnapshot, error) {
	for i := range manifests {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("protect manifest metadata for cleanup: %w", err)
		}
		manifest := &manifests[i]
		add(protected, manifest.Digest, manifest.ObjectKey)
	}
	if err := s.protectManifestGraph(ctx, manifests, protected, false); err != nil {
		return nil, err
	}
	return &referenceSnapshot{protected: protected}, nil
}

func protectRecentBlobActivity(ctx context.Context, cutoff time.Time, blobs []meta.BlobRecord, protected *set.Set[string]) error {
	for i := range blobs {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("protect blob activity for cleanup: %w", err)
		}
		blob := &blobs[i]
		if isRecent(cutoff, blob.LastAccessAt, blob.UpdatedAt, blob.CreatedAt) {
			add(protected, blob.Digest, blob.ObjectKey)
		}
	}
	return nil
}

func activePullReferences(ctx context.Context, cutoff time.Time, pulls []meta.PullRecord) (map[string]bool, error) {
	active := make(map[string]bool)
	for i := range pulls {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("read pull activity for cleanup: %w", err)
		}
		pull := &pulls[i]
		if isRecent(cutoff, pull.LastPullAt, pull.UpdatedAt, pull.CreatedAt) {
			active[refKey(pull.Alias, pull.Repository, pull.Reference)] = true
		}
	}
	return active, nil
}

func activeTagReferences(ctx context.Context, cutoff time.Time, tags []meta.TagRecord, activePulls map[string]bool) (map[string]bool, map[string]bool, error) {
	activeTags := make(map[string]bool, len(tags))
	activeManifests := make(map[string]bool)
	for i := range tags {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("read tag activity for cleanup: %w", err)
		}
		tag := &tags[i]
		active := isRecent(cutoff, tag.UpdatedAt, tag.CreatedAt) || activePulls[refKey(tag.Alias, tag.Repository, tag.Reference)]
		activeTags[tagKey(*tag)] = active
		if active {
			activeManifests[manifestKey(tag.Alias, tag.Repository, tag.Digest)] = true
		}
	}
	return activeTags, activeManifests, nil
}

func classifyManifestReferences(ctx context.Context, cutoff time.Time, manifests []meta.ManifestRecord, activeKeys map[string]bool, out *referenceSnapshot) error {
	for i := range manifests {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read manifest activity for cleanup: %w", err)
		}
		manifest := &manifests[i]
		active := isRecent(cutoff, manifest.UpdatedAt, manifest.CreatedAt) || activeKeys[manifestKey(manifest.Alias, manifest.Repository, manifest.Digest)]
		if active {
			add(out.protected, manifest.Digest, manifest.ObjectKey)
			continue
		}
		stale := *manifest
		stale.Headers = nil
		out.staleManifests = append(out.staleManifests, stale)
	}
	return nil
}

func classifyTagReferences(ctx context.Context, tags []meta.TagRecord, activeTags map[string]bool, out *referenceSnapshot) error {
	for i := range tags {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read stale tags for cleanup: %w", err)
		}
		tag := &tags[i]
		if activeTags[tagKey(*tag)] {
			add(out.protected, tag.Digest)
			continue
		}
		out.staleTags = append(out.staleTags, *tag)
	}
	return nil
}

func classifyRepoBlobReferences(ctx context.Context, cutoff time.Time, links []meta.RepoBlobRecord, out *referenceSnapshot) error {
	for i := range links {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read stale repository links for cleanup: %w", err)
		}
		link := &links[i]
		if isRecent(cutoff, link.LastAccessAt, link.LastVerifiedAt, link.UpdatedAt, link.CreatedAt) {
			add(out.protected, link.Digest)
			continue
		}
		out.staleRepoBlobs = append(out.staleRepoBlobs, *link)
	}
	return nil
}

func add(valuesSet *set.Set[string], values ...string) {
	for _, v := range values {
		if v != "" {
			valuesSet.Add(v)
		}
	}
}
func isRecent(cutoff time.Time, values ...time.Time) bool {
	found := false
	var latest time.Time
	for _, v := range values {
		if v.IsZero() {
			continue
		}
		if !found || v.After(latest) {
			latest = v
			found = true
		}
	}
	return !found || !latest.Before(cutoff)
}
func refKey(alias, repo, ref string) string { return alias + "\x00" + repo + "\x00" + ref }
func tagKey(t meta.TagRecord) string        { return refKey(t.Alias, t.Repository, t.Reference) }
func manifestKey(alias, repository, digest string) string {
	return alias + "\x00" + repository + "\x00" + digest
}
