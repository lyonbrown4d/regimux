package prefetch

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

// Sync fetches one manifest and its directly referenced artifacts into cache.
func (s *Service) Sync(ctx context.Context, opts manualsync.SyncOptions) (*manualsync.SyncReport, error) {
	if err := s.validateSync(ctx, opts); err != nil {
		return nil, err
	}
	if opts.Accept == "" {
		opts.Accept = distribution.DefaultManifestAccept
	}

	startedAt := time.Now()
	s.logger.InfoContext(ctx, "manual sync starting", "alias", opts.Alias, "artifact", opts.Artifact, "reference", opts.Reference)
	candidate := Candidate{
		Alias:  opts.Alias,
		Repo:   opts.Artifact,
		Tag:    opts.Reference,
		Reason: "manual sync",
	}
	manifest, err := s.refreshManifest(ctx, cache.ManifestRequest{
		UpstreamAlias:  opts.Alias,
		Repo:           opts.Artifact,
		Reference:      opts.Reference,
		Accept:         opts.Accept,
		SkipPullRecord: true,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "manual sync manifest failed", "alias", opts.Alias, "artifact", opts.Artifact, "reference", opts.Reference, "error", err)
		return nil, cacheWrap(err, "sync manifest")
	}

	result, err := s.prefetchManifestArtifacts(ctx, RunOptions{Accept: opts.Accept}, nil, candidate, opts.Reference, manifest, 0)
	if err != nil {
		s.logger.WarnContext(ctx, "manual sync artifacts failed", "alias", opts.Alias, "artifact", opts.Artifact, "reference", opts.Reference, "error", err)
		return nil, err
	}
	duration := time.Since(startedAt)
	s.logger.InfoContext(ctx,
		"manual sync completed",
		"alias", opts.Alias,
		"artifact", opts.Artifact,
		"reference", opts.Reference,
		"manifest_digest", result.manifestDigest,
		"layers", result.layerCount,
		"blobs", result.blobCount,
		"child_manifests", result.childManifestCount,
		"duration", duration,
	)
	return &manualsync.SyncReport{
		Alias:              opts.Alias,
		Artifact:           opts.Artifact,
		Reference:          opts.Reference,
		Digest:             result.manifestDigest,
		MediaType:          cachedManifestMediaType(manifest),
		LayerCount:         result.layerCount,
		BlobCount:          result.blobCount,
		ChildManifestCount: result.childManifestCount,
		Duration:           duration,
	}, nil
}

func (s *Service) validateSync(ctx context.Context, opts manualsync.SyncOptions) error {
	if ctx == nil {
		return cacheError("sync context is required")
	}
	if err := ctx.Err(); err != nil {
		return cacheWrap(err, "sync context")
	}
	if s == nil || s.manifests == nil {
		return cacheError("sync service is not configured")
	}
	if opts.Alias == "" {
		return cacheError("sync upstream alias is required")
	}
	if opts.Artifact == "" {
		return cacheError("sync artifact is required")
	}
	if opts.Reference == "" {
		return cacheError("sync reference is required")
	}
	return nil
}
