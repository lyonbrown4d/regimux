package cachegc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const manifestReadLimit = 4 << 20

type referenceSnapshot struct {
	protected      *set.Set[string]
	staleTags      []meta.TagRecord
	staleManifests []meta.ManifestRecord
	staleRepoBlobs []meta.RepoBlobRecord
}

//nolint:cyclop,gocognit,gocyclo,funlen // This orchestrates the shared metadata snapshots and preserves conservative legacy roots.
func (s *Service) loadReferences(ctx context.Context, cutoff time.Time, prune bool) (*referenceSnapshot, error) {
	out := &referenceSnapshot{protected: set.NewSet[string]()}
	manifests, err := s.metadata.ListManifests(ctx)
	if err != nil {
		return nil, fmt.Errorf("list manifest metadata for cleanup: %w", err)
	}
	tags, err := s.metadata.ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tag metadata for cleanup: %w", err)
	}
	pulls, err := s.metadata.ListPulls(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pull metadata for cleanup: %w", err)
	}
	repoBlobs, err := s.metadata.ListRepoBlobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repository blob metadata for cleanup: %w", err)
	}
	if !prune {
		if manifests != nil {
			manifests.Range(func(_ int, m meta.ManifestRecord) bool { add(out.protected, m.Digest, m.ObjectKey); return true })
		}
		if graphErr := s.protectManifestGraph(ctx, manifests, out.protected, false); graphErr != nil {
			return nil, graphErr
		}
		return out, nil
	}
	// A blob touch is ecosystem-neutral: recent access through another alias or
	// a manifest cache hit must keep the OCI graph rooted at that content digest.
	blobs, err := s.metadata.ListBlobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list blob activity for cleanup: %w", err)
	}
	if blobs != nil {
		blobs.Range(func(_ int, blob meta.BlobRecord) bool {
			if isRecent(cutoff, blob.LastAccessAt, blob.UpdatedAt, blob.CreatedAt) {
				add(out.protected, blob.Digest, blob.ObjectKey)
			}
			return true
		})
	}

	activePulls := map[string]bool{}
	if pulls != nil {
		pulls.Range(func(_ int, p meta.PullRecord) bool {
			if isRecent(cutoff, p.LastPullAt, p.UpdatedAt, p.CreatedAt) {
				activePulls[refKey(p.Alias, p.Repository, p.Reference)] = true
			}
			return true
		})
	}
	activeTags := map[string]bool{}
	activeManifestKeys := map[string]bool{}
	if tags != nil {
		tags.Range(func(_ int, t meta.TagRecord) bool {
			active := isRecent(cutoff, t.UpdatedAt, t.CreatedAt) || activePulls[refKey(t.Alias, t.Repository, t.Reference)]
			activeTags[tagKey(t)] = active
			if active {
				activeManifestKeys[manifestKey(t.Alias, t.Repository, t.Digest)] = true
			}
			return true
		})
	}
	if manifests != nil {
		manifests.Range(func(_ int, m meta.ManifestRecord) bool {
			active := isRecent(cutoff, m.UpdatedAt, m.CreatedAt) || activeManifestKeys[manifestKey(m.Alias, m.Repository, m.Digest)]
			if active {
				add(out.protected, m.Digest, m.ObjectKey)
			} else {
				out.staleManifests = append(out.staleManifests, m)
			}
			return true
		})
	}
	if tags != nil {
		tags.Range(func(_ int, t meta.TagRecord) bool {
			if activeTags[tagKey(t)] {
				add(out.protected, t.Digest)
			} else {
				out.staleTags = append(out.staleTags, t)
			}
			return true
		})
	}
	if repoBlobs != nil {
		repoBlobs.Range(func(_ int, r meta.RepoBlobRecord) bool {
			if isRecent(cutoff, r.LastAccessAt, r.LastVerifiedAt, r.UpdatedAt, r.CreatedAt) {
				add(out.protected, r.Digest)
			} else {
				out.staleRepoBlobs = append(out.staleRepoBlobs, r)
			}
			return true
		})
	}
	if manifests != nil {
		if graphErr := s.protectManifestGraph(ctx, manifests, out.protected, true); graphErr != nil {
			return nil, graphErr
		}
	}
	return out, nil
}

//nolint:cyclop,gocognit,gocyclo // Explicit cycle detection and fail-closed parsing are central to this graph walk.
func (s *Service) protectManifestGraph(ctx context.Context, manifests *list.List[meta.ManifestRecord], protected *set.Set[string], strict bool) error {
	if manifests == nil {
		return nil
	}
	byDigest := map[string][]meta.ManifestRecord{}
	manifests.Range(func(_ int, m meta.ManifestRecord) bool {
		byDigest[m.Digest] = append(byDigest[m.Digest], m)
		return true
	})
	queue := make([]string, 0, protected.Len())
	protected.Range(func(d string) bool { queue = append(queue, d); return true })
	visited := set.NewSet[string]()
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if visited.Contains(d) {
			continue
		}
		visited.Add(d)
		records := byDigest[d]
		for i := range records {
			m := records[i]
			add(protected, m.Digest, m.ObjectKey)
			if !needsOCITraversal(m) {
				continue
			}
			body, err := s.readManifest(ctx, m)
			if err != nil {
				if strict {
					return fmt.Errorf("read manifest graph for cleanup (%s): %w", m.Digest, err)
				}
				continue
			}
			children, parseErr := manifestReferences(m.MediaType, body)
			if parseErr != nil {
				if strict {
					return fmt.Errorf("parse manifest graph for cleanup (%s): %w", m.Digest, parseErr)
				}
				continue
			}
			for _, child := range children {
				if !protected.Contains(child) {
					protected.Add(child)
					queue = append(queue, child)
				}
			}
		}
	}
	return nil
}

func (s *Service) readManifest(ctx context.Context, m meta.ManifestRecord) ([]byte, error) {
	key := m.ObjectKey
	if key == "" {
		key = m.Digest
	}
	if key == "" {
		return nil, nil
	}
	r, _, err := s.objects.Get(ctx, key, object.GetOptions{})
	if errors.Is(err, object.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get manifest object for cleanup: %w", err)
	}
	if r == nil {
		return nil, nil
	}
	body, readErr := io.ReadAll(io.LimitReader(r, manifestReadLimit+1))
	closeErr := r.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read manifest object for cleanup: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close manifest object for cleanup: %w", closeErr)
	}
	if len(body) > manifestReadLimit {
		return nil, errors.New("manifest exceeds cleanup read bound")
	}
	return body, nil
}

func needsOCITraversal(manifest meta.ManifestRecord) bool {
	if isOCIManifestType(manifest.MediaType) {
		return true
	}
	// A missing media type is ambiguous only for manifest-sized records. Large
	// package artifacts are opaque and must not block a global cleanup sweep.
	return manifest.MediaType == "" && manifest.Size > 0 && manifest.Size <= manifestReadLimit
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
