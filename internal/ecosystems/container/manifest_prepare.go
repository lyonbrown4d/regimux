package container

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
)

func (e *RegistryEndpoint) prepareManifest(
	ctx context.Context,
	req cache.ManifestRequest,
) (*cache.CachedManifest, cache.ManifestCompletion, error) {
	if preparer, ok := e.manifests.(cache.ManifestPreparer); ok {
		manifest, complete, err := preparer.Prepare(ctx, req)
		return manifest, complete, wrapError(err, "prepare manifest metadata")
	}
	manifest, err := e.manifests.Get(ctx, req)
	return manifest, nil, wrapError(err, "get manifest metadata")
}

func manifestCompletion(ctx context.Context, complete cache.ManifestCompletion, fill *manifestBlobFillPlan) func() error {
	return func() error {
		if fill != nil {
			defer fill.discard()
		}
		if complete != nil {
			if err := complete.CompleteResponse(ctx); err != nil {
				return wrapError(err, "complete prepared manifest response")
			}
		}
		if fill != nil {
			fill.submit(ctx)
		}
		return nil
	}
}
