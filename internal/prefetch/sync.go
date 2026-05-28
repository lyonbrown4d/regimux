package prefetch

import (
	"context"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

// SyncOptions identifies one manifest reference to prefetch explicitly.
type SyncOptions struct {
	Alias     string
	Repo      string
	Reference string
	Accept    string
}

// SyncReport summarizes artifacts warmed by a manual sync.
type SyncReport struct {
	Alias              string
	Repo               string
	Reference          string
	ManifestDigest     string
	MediaType          string
	LayerCount         int
	BlobCount          int
	ChildManifestCount int
	Duration           time.Duration
}

// Sync fetches one manifest and its directly referenced artifacts into cache.
func (s *Service) Sync(ctx context.Context, opts SyncOptions) (*SyncReport, error) {
	if err := s.validateSync(ctx, opts); err != nil {
		return nil, err
	}
	if opts.Accept == "" {
		opts.Accept = distribution.DefaultManifestAccept
	}

	startedAt := time.Now()
	candidate := Candidate{
		Alias:  opts.Alias,
		Repo:   opts.Repo,
		Tag:    opts.Reference,
		Reason: "manual sync",
	}
	manifest, err := s.manifests.Get(ctx, cache.ManifestRequest{
		UpstreamAlias:  opts.Alias,
		Repo:           opts.Repo,
		Reference:      opts.Reference,
		Accept:         opts.Accept,
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return nil, cacheWrap(err, "sync manifest")
	}

	result, err := s.prefetchManifestArtifacts(ctx, RunOptions{Accept: opts.Accept}, candidate, opts.Reference, manifest, 0)
	if err != nil {
		return nil, err
	}
	return &SyncReport{
		Alias:              opts.Alias,
		Repo:               opts.Repo,
		Reference:          opts.Reference,
		ManifestDigest:     result.manifestDigest,
		MediaType:          cachedManifestMediaType(manifest),
		LayerCount:         result.layerCount,
		BlobCount:          result.blobCount,
		ChildManifestCount: result.childManifestCount,
		Duration:           time.Since(startedAt),
	}, nil
}

func (s *Service) validateSync(ctx context.Context, opts SyncOptions) error {
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
	if opts.Repo == "" {
		return cacheError("sync repository is required")
	}
	if opts.Reference == "" {
		return cacheError("sync reference is required")
	}
	return nil
}
