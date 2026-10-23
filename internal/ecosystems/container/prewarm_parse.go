package container

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
)

func (e *RegistryEndpoint) prepareImageBlobFillPlan(
	ctx context.Context,
	route reference.Route,
	manifest *cache.CachedManifest,
	mediaType string,
	release func(),
) *manifestBlobFillPlan {
	manifestDigest := manifest.Digest
	descriptors, err := imageManifestBlobDescriptors(manifest)
	if err != nil {
		e.logger.WarnContext(ctx, "decode image manifest for blob fill failed",
			"alias", route.Alias,
			"repository", route.Repo,
			"reference", route.Reference,
			"digest", manifestDigest,
			"media_type", mediaType,
			"error", err,
		)
		return nil
	}
	return newManifestBlobFillPlan(ctx, &manifestBlobFillWork{
		endpoint: e, route: route, mediaType: mediaType, digest: manifestDigest,
		task: func(taskCtx context.Context) {
			e.fillManifestBlobs(taskCtx, route, manifestDigest, descriptors)
		},
	}, release)
}

func (e *RegistryEndpoint) prepareIndexBlobFillPlan(
	ctx context.Context,
	route reference.Route,
	manifest *cache.CachedManifest,
	mediaType string,
	release func(),
) *manifestBlobFillPlan {
	manifestDigest := manifest.Digest
	selected, err := prewarmManifestChildren(route.Alias, manifest)
	if err != nil {
		e.logger.WarnContext(ctx, "decode image index for blob fill failed",
			"alias", route.Alias,
			"repository", route.Repo,
			"reference", route.Reference,
			"digest", manifestDigest,
			"media_type", mediaType,
			"error", err,
		)
		return nil
	}
	if len(selected) == 0 {
		e.logSkippedManifestBlobFill(ctx, route, manifestDigest, mediaType, "no configured platform child manifest")
		return nil
	}
	return newManifestBlobFillPlan(ctx, &manifestBlobFillWork{
		endpoint: e, route: route, mediaType: mediaType, digest: manifestDigest,
		task: func(taskCtx context.Context) {
			e.fillManifestBlobsForIndex(taskCtx, route, manifestDigest, selected)
		},
	}, release)
}
