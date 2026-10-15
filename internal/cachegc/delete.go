package cachegc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func (s *Service) deleteBlob(ctx context.Context, opts CleanupOptions, cutoff time.Time, digest string, refs *referenceSnapshot, report *CleanupReport) (bool, error) {
	blob, currentRefs, allowed, err := s.loadEligibleBlob(ctx, opts, cutoff, digest, report)
	if err != nil || !allowed {
		return false, err
	}
	*refs = *currentRefs
	stat, statErr := s.statCandidate(ctx, digest)
	if statErr != nil {
		return false, statErr
	}
	latestBlob, currentRefs, allowed, err := s.confirmEligibleBlob(ctx, opts, cutoff, *blob, report)
	if err != nil || !allowed {
		return false, err
	}
	*refs = *currentRefs
	if deleteErr := s.objects.Delete(ctx, digest); deleteErr != nil {
		return false, fmt.Errorf("delete blob object for cleanup: %w", deleteErr)
	}
	deleted, err := s.finishBlobDeletion(ctx, opts, digest, *latestBlob, currentRefs)
	if err != nil || !deleted {
		return false, err
	}
	report.BytesDeleted += stat.size
	if stat.missing {
		report.MissingObjects++
	}
	report.DeletedBlobs++
	report.DeletedDigests = append(report.DeletedDigests, digest)
	report.BytesAfter = remaining(report.BytesAfter, blob.Size)
	*refs = *currentRefs
	return true, nil
}

func (s *Service) loadEligibleBlob(ctx context.Context, opts CleanupOptions, cutoff time.Time, digest string, report *CleanupReport) (*meta.BlobRecord, *referenceSnapshot, bool, error) {
	blob, ok, err := s.metadata.Blob(ctx, meta.BlobKey{Digest: digest})
	if err != nil {
		return nil, nil, false, fmt.Errorf("recheck blob metadata before cleanup: %w", err)
	}
	if !ok {
		return nil, nil, false, nil
	}
	refs, err := s.loadReferences(ctx, cutoff, opts.PruneReferences)
	if err != nil {
		return nil, nil, false, err
	}
	if !recordEligibleOrCapacity(*blob, opts, cutoff, refs, report) {
		recordSkip(report, classify(*blob, cutoff, refs.protected, opts.PruneReferences))
		return nil, refs, false, nil
	}
	return blob, refs, true, nil
}

func recordEligibleOrCapacity(blob meta.BlobRecord, opts CleanupOptions, cutoff time.Time, refs *referenceSnapshot, report *CleanupReport) bool {
	reason := classify(blob, cutoff, refs.protected, opts.PruneReferences)
	return candidateEligible(opts, report, reason)
}

type candidateStat struct {
	size    int64
	missing bool
}

func (s *Service) statCandidate(ctx context.Context, digest string) (candidateStat, error) {
	info, err := s.objects.Stat(ctx, digest)
	if err == nil {
		return candidateStat{size: info.Size}, nil
	}
	if errors.Is(err, object.ErrNotFound) {
		return candidateStat{missing: true}, nil
	}
	return candidateStat{}, fmt.Errorf("stat blob object for cleanup: %w", err)
}

func (s *Service) confirmEligibleBlob(ctx context.Context, opts CleanupOptions, cutoff time.Time, original meta.BlobRecord, report *CleanupReport) (*meta.BlobRecord, *referenceSnapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, false, fmt.Errorf("cleanup context: %w", err)
	}
	current, ok, err := s.metadata.Blob(ctx, meta.BlobKey{Digest: original.Digest})
	if err != nil {
		return nil, nil, false, fmt.Errorf("recheck blob metadata after stat: %w", err)
	}
	if !ok || !sameBlobRevision(original, *current) {
		return nil, nil, false, nil
	}
	refs, err := s.loadReferences(ctx, cutoff, opts.PruneReferences)
	if err != nil {
		return nil, nil, false, err
	}
	if !recordEligibleOrCapacity(*current, opts, cutoff, refs, report) {
		recordSkip(report, classify(*current, cutoff, refs.protected, opts.PruneReferences))
		return nil, refs, false, nil
	}
	return current, refs, true, nil
}

func (s *Service) finishBlobDeletion(ctx context.Context, opts CleanupOptions, digest string, blob meta.BlobRecord, refs *referenceSnapshot) (bool, error) {
	if opts.PruneReferences {
		pruned, err := s.pruneReferencesForDigest(ctx, digest, refs)
		if err != nil || !pruned {
			return false, err
		}
	}
	deleted, err := deleteBlobMetadata(ctx, s.metadata, blob)
	if err != nil {
		return false, fmt.Errorf("delete blob metadata for cleanup: %w", err)
	}
	return deleted, nil
}

func deleteBlobMetadata(ctx context.Context, store meta.Store, expected meta.BlobRecord) (bool, error) {
	if conditional, ok := store.(meta.ConditionalGCDeleter); ok {
		deleted, err := conditional.DeleteBlobIfUnchanged(ctx, expected)
		if err != nil {
			return false, fmt.Errorf("conditional delete blob metadata: %w", err)
		}
		return deleted, nil
	}
	current, ok, err := store.Blob(ctx, meta.BlobKey{Digest: expected.Digest})
	if err != nil {
		return false, fmt.Errorf("read blob metadata for conditional delete: %w", err)
	}
	if !ok || !sameBlobRevision(*current, expected) {
		return false, nil
	}
	if err := store.DeleteBlob(ctx, meta.BlobKey{Digest: expected.Digest}); err != nil {
		return false, fmt.Errorf("delete blob metadata: %w", err)
	}
	return true, nil
}

func sameBlobRevision(left, right meta.BlobRecord) bool {
	return left.Digest == right.Digest && left.Size == right.Size && left.ObjectKey == right.ObjectKey && left.LastAccessAt.Equal(right.LastAccessAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func (s *Service) pruneReferencesForDigest(ctx context.Context, digest string, refs *referenceSnapshot) (bool, error) {
	for _, prune := range []func(context.Context, string, *referenceSnapshot) (bool, error){
		s.pruneTags,
		s.pruneManifests,
		s.pruneRepoBlobs,
	} {
		pruned, err := prune(ctx, digest, refs)
		if err != nil || !pruned {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) pruneTags(ctx context.Context, digest string, refs *referenceSnapshot) (bool, error) {
	for i := range refs.staleTags {
		t := &refs.staleTags[i]
		if t.Digest == digest {
			deleted, err := deleteTagIfUnchanged(ctx, s.metadata, *t)
			if err != nil {
				return false, fmt.Errorf("delete stale tag reference: %w", err)
			}
			if !deleted {
				return false, nil
			}
		}
	}
	return true, nil
}

func (s *Service) pruneManifests(ctx context.Context, digest string, refs *referenceSnapshot) (bool, error) {
	for i := range refs.staleManifests {
		m := &refs.staleManifests[i]
		if m.Digest == digest {
			deleted, err := deleteManifestIfUnchanged(ctx, s.metadata, *m)
			if err != nil {
				return false, fmt.Errorf("delete stale manifest reference: %w", err)
			}
			if !deleted {
				return false, nil
			}
		}
	}
	return true, nil
}

func (s *Service) pruneRepoBlobs(ctx context.Context, digest string, refs *referenceSnapshot) (bool, error) {
	for i := range refs.staleRepoBlobs {
		r := &refs.staleRepoBlobs[i]
		if r.Digest == digest {
			deleted, err := deleteRepoBlobIfUnchanged(ctx, s.metadata, *r)
			if err != nil {
				return false, fmt.Errorf("delete stale repository blob reference: %w", err)
			}
			if !deleted {
				return false, nil
			}
		}
	}
	return true, nil
}

//nolint:dupl // Tag and repository-blob keys/revisions differ, so entity-specific compare-delete stays typed.
func deleteTagIfUnchanged(ctx context.Context, store meta.Store, expected meta.TagRecord) (bool, error) {
	if conditional, ok := store.(meta.ConditionalGCDeleter); ok {
		deleted, err := conditional.DeleteTagIfUnchanged(ctx, expected)
		if err != nil {
			return false, fmt.Errorf("conditionally delete tag reference: %w", err)
		}
		return deleted, nil
	}
	key := meta.TagKey{Alias: expected.Alias, Repository: expected.Repository, Reference: expected.Reference}
	current, ok, err := store.Tag(ctx, key)
	if err != nil {
		return false, fmt.Errorf("read tag reference before delete: %w", err)
	}
	if !ok || current.Digest != expected.Digest || !current.UpdatedAt.Equal(expected.UpdatedAt) {
		return false, nil
	}
	if err := store.DeleteTag(ctx, key); err != nil {
		return false, fmt.Errorf("delete tag reference: %w", err)
	}
	return true, nil
}

func deleteManifestIfUnchanged(ctx context.Context, store meta.Store, expected meta.ManifestRecord) (bool, error) {
	if conditional, ok := store.(meta.ConditionalGCDeleter); ok {
		deleted, err := conditional.DeleteManifestIfUnchanged(ctx, expected)
		if err != nil {
			return false, fmt.Errorf("conditionally delete manifest reference: %w", err)
		}
		return deleted, nil
	}
	key := meta.ManifestKey{Alias: expected.Alias, Repository: expected.Repository, Digest: expected.Digest}
	current, ok, err := store.Manifest(ctx, key)
	if err != nil {
		return false, fmt.Errorf("read manifest reference before delete: %w", err)
	}
	if !ok || current.Digest != expected.Digest || current.ObjectKey != expected.ObjectKey || !current.UpdatedAt.Equal(expected.UpdatedAt) {
		return false, nil
	}
	if err := store.DeleteManifest(ctx, key); err != nil {
		return false, fmt.Errorf("delete manifest reference: %w", err)
	}
	return true, nil
}

//nolint:dupl // Repository blob keys and revisions differ from tag references and retain separate compare-delete semantics.
func deleteRepoBlobIfUnchanged(ctx context.Context, store meta.Store, expected meta.RepoBlobRecord) (bool, error) {
	if conditional, ok := store.(meta.ConditionalGCDeleter); ok {
		deleted, err := conditional.DeleteRepoBlobIfUnchanged(ctx, expected)
		if err != nil {
			return false, fmt.Errorf("conditionally delete repository blob reference: %w", err)
		}
		return deleted, nil
	}
	key := meta.RepoBlobKey{Alias: expected.Alias, Repository: expected.Repository, Digest: expected.Digest}
	current, ok, err := store.RepoBlob(ctx, key)
	if err != nil {
		return false, fmt.Errorf("read repository blob reference before delete: %w", err)
	}
	if !ok || current.Digest != expected.Digest || !current.UpdatedAt.Equal(expected.UpdatedAt) {
		return false, nil
	}
	if err := store.DeleteRepoBlob(ctx, key); err != nil {
		return false, fmt.Errorf("delete repository blob reference: %w", err)
	}
	return true, nil
}
