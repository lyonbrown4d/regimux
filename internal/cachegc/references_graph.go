package cachegc

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const manifestReadLimit = 4 << 20

func (s *Service) protectManifestGraph(ctx context.Context, manifests []meta.ManifestRecord, protected *set.Set[string], strict bool) error {
	walk, err := newManifestGraphWalk(ctx, manifests, protected, strict)
	if err != nil {
		return err
	}
	for len(walk.queue) > 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("walk manifest graph for cleanup: %w", err)
		}
		digest := walk.pop()
		if walk.visited.Contains(digest) {
			continue
		}
		walk.visited.Add(digest)
		if err := s.protectManifestDigest(ctx, digest, walk); err != nil {
			return err
		}
	}
	return nil
}

type manifestGraphWalk struct {
	byDigest  map[string][]meta.ManifestRecord
	protected *set.Set[string]
	visited   *set.Set[string]
	queue     []string
	strict    bool
	limits    meta.CleanupSnapshotLimits
	records   int
	bytes     int64
}

func newManifestGraphWalk(
	ctx context.Context,
	manifests []meta.ManifestRecord,
	protected *set.Set[string],
	strict bool,
) (*manifestGraphWalk, error) {
	limits := meta.CleanupSnapshotLimitsFromContext(ctx)
	queueCapacity := protected.Len()
	queueCapacity = min(queueCapacity, limits.MaxRecords)
	walk := &manifestGraphWalk{
		byDigest:  indexManifestRecords(manifests),
		protected: protected,
		visited:   set.NewSet[string](),
		queue:     make([]string, 0, queueCapacity),
		strict:    strict,
		limits:    limits,
	}
	var rootErr error
	protected.Range(func(digest string) bool {
		if err := ctx.Err(); err != nil {
			rootErr = fmt.Errorf("walk manifest graph for cleanup: %w", err)
			return false
		}
		if rootErr = walk.reserve(digest); rootErr != nil {
			return false
		}
		walk.queue = append(walk.queue, digest)
		return true
	})
	if rootErr != nil {
		return nil, rootErr
	}
	return walk, nil
}

func (walk *manifestGraphWalk) reserve(digest string) error {
	bytes := int64(len(digest))
	if walk.records >= walk.limits.MaxRecords || bytes > walk.limits.MaxBytes-walk.bytes {
		return fmt.Errorf("%w: relation=manifest graph records=%d/%d bytes=%d/%d",
			meta.ErrCleanupSnapshotLimit, walk.records+1, walk.limits.MaxRecords,
			walk.bytes+bytes, walk.limits.MaxBytes)
	}
	walk.records++
	walk.bytes += bytes
	return nil
}

func indexManifestRecords(manifests []meta.ManifestRecord) map[string][]meta.ManifestRecord {
	byDigest := make(map[string][]meta.ManifestRecord)
	for i := range manifests {
		manifest := manifests[i]
		byDigest[manifest.Digest] = append(byDigest[manifest.Digest], manifest)
	}
	return byDigest
}

func (walk *manifestGraphWalk) pop() string {
	digest := walk.queue[0]
	walk.queue[0] = ""
	walk.queue = walk.queue[1:]
	return digest
}

func (s *Service) protectManifestDigest(ctx context.Context, digest string, walk *manifestGraphWalk) error {
	for i := range walk.byDigest[digest] {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("walk manifest graph for cleanup: %w", err)
		}
		manifest := walk.byDigest[digest][i]
		if err := walk.protect(ctx, manifest.Digest, manifest.ObjectKey); err != nil {
			return fmt.Errorf("walk manifest graph for cleanup: %w", err)
		}
		children, err := s.readManifestChildren(ctx, manifest, walk.strict)
		if err != nil {
			return err
		}
		if err := walk.enqueue(ctx, children); err != nil {
			return fmt.Errorf("walk manifest graph for cleanup: %w", err)
		}
	}
	return nil
}

func (s *Service) readManifestChildren(
	ctx context.Context,
	manifest meta.ManifestRecord,
	strict bool,
) ([]string, error) {
	if !needsOCITraversal(manifest) {
		return nil, nil
	}
	body, err := s.readManifest(ctx, manifest)
	if err != nil {
		if strict {
			return nil, fmt.Errorf("read manifest graph for cleanup (%s): %w", manifest.Digest, err)
		}
		return nil, nil
	}
	children, err := manifestReferences(manifest.MediaType, body)
	if err != nil {
		if strict || errors.Is(err, jsonbudget.ErrLimit) || errors.Is(err, jsonbudget.ErrInvalidJSON) {
			return nil, fmt.Errorf("parse manifest graph for cleanup (%s): %w", manifest.Digest, err)
		}
		return nil, nil
	}
	return children, nil
}

func (walk *manifestGraphWalk) enqueue(ctx context.Context, children []string) error {
	for _, child := range children {
		if err := walk.protect(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

func (walk *manifestGraphWalk) protect(ctx context.Context, digests ...string) error {
	for _, digest := range digests {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("protect manifest graph references: %w", err)
		}
		if digest == "" || walk.protected.Contains(digest) {
			continue
		}
		if err := walk.reserve(digest); err != nil {
			return err
		}
		walk.protected.Add(digest)
		walk.queue = append(walk.queue, digest)
	}
	return nil
}

func (s *Service) readManifest(ctx context.Context, manifest meta.ManifestRecord) ([]byte, error) {
	key := manifest.ObjectKey
	if key == "" {
		key = manifest.Digest
	}
	if key == "" {
		return nil, nil
	}
	r, _, err := s.objects.Get(ctx, key, object.GetOptions{})
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
