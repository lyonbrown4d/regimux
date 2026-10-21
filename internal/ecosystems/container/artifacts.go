package container

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (e *RegistryEndpoint) prepareManifestBlobFill(
	ctx context.Context,
	route reference.Route,
	manifest *cache.CachedManifest,
) func() {
	if e == nil || e.blobs == nil || manifest == nil {
		return nil
	}
	mediaType := cachedManifestMediaType(manifest)
	manifestDigest := manifest.Digest
	if isImageManifestMediaType(mediaType) {
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
		return func() {
			e.submitManifestBlobFill(ctx, route, mediaType, manifestDigest, func(taskCtx context.Context) {
				e.fillManifestBlobs(taskCtx, route, manifestDigest, descriptors)
			})
		}
	}
	if isIndexManifestMediaType(mediaType) {
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
		return func() {
			e.submitManifestBlobFill(ctx, route, mediaType, manifestDigest, func(taskCtx context.Context) {
				e.fillManifestBlobsForIndex(taskCtx, route, manifestDigest, selected)
			})
		}
	}
	e.logSkippedManifestBlobFill(ctx, route, manifestDigest, mediaType, "unsupported manifest media type")
	return nil
}

func (e *RegistryEndpoint) submitManifestBlobFill(
	ctx context.Context,
	route reference.Route,
	mediaType string,
	manifestDigest string,
	task func(context.Context),
) {
	if task == nil {
		return
	}
	taskCtx := context.WithoutCancel(ctx)
	if e.workers == nil {
		e.logSkippedManifestBlobFill(ctx, route, manifestDigest, mediaType, "worker pool unavailable")
		return
	}
	pool := e.workers.IOPool()
	if pool == nil {
		e.logSkippedManifestBlobFill(ctx, route, manifestDigest, mediaType, "worker pool unavailable")
		return
	}
	if pool.Free() == 0 {
		e.logSkippedManifestBlobFill(ctx, route, manifestDigest, mediaType, "worker pool saturated")
		return
	}
	if err := pool.Submit(func() {
		task(taskCtx)
	}); err != nil {
		e.publishWorkerFillSkipped(ctx, route, "blob", "submit failed")
		e.logger.DebugContext(ctx, "submit manifest blob fill failed",
			"alias", route.Alias,
			"repository", route.Repo,
			"reference", route.Reference,
			"digest", manifestDigest,
			"error", err,
		)
	}
}

func (e *RegistryEndpoint) fillManifestBlobs(
	ctx context.Context,
	route reference.Route,
	manifestDigest string,
	descriptors []manifestBlobDescriptor,
) {
	for i := range descriptors {
		descriptor := descriptors[i]
		if descriptor.digest == "" {
			e.logSkippedManifestBlob(ctx, route, manifestDigest, descriptor, "empty digest")
			continue
		}
		result, err := e.blobs.Get(ctx, cache.BlobRequest{
			UpstreamAlias: route.Alias,
			Repo:          route.Repo,
			Digest:        descriptor.digest,
			Method:        http.MethodGet,
		})
		if err != nil {
			e.logger.WarnContext(ctx, "manifest blob fill failed",
				"alias", route.Alias,
				"repository", route.Repo,
				"reference", route.Reference,
				"manifest_digest", manifestDigest,
				"digest", descriptor.digest,
				"media_type", descriptor.mediaType,
				"size", descriptor.size,
				"kind", descriptor.kind,
				"error", err,
			)
			continue
		}
		e.drainFilledBlob(ctx, route, manifestDigest, descriptor, result)
	}
}

func (e *RegistryEndpoint) drainFilledBlob(
	ctx context.Context,
	route reference.Route,
	manifestDigest string,
	descriptor manifestBlobDescriptor,
	result *cache.BlobReadResult,
) {
	if result == nil || result.Reader == nil {
		return
	}
	if _, err := io.Copy(io.Discard, result.Reader); err != nil {
		e.logger.WarnContext(ctx, "read manifest blob fill failed",
			"alias", route.Alias,
			"repository", route.Repo,
			"reference", route.Reference,
			"manifest_digest", manifestDigest,
			"digest", descriptor.digest,
			"media_type", descriptor.mediaType,
			"size", descriptor.size,
			"kind", descriptor.kind,
			"error", err,
		)
	}
	if err := result.Reader.Close(); err != nil {
		e.logger.WarnContext(ctx, "close manifest blob fill reader failed",
			"alias", route.Alias,
			"repository", route.Repo,
			"reference", route.Reference,
			"manifest_digest", manifestDigest,
			"digest", descriptor.digest,
			"media_type", descriptor.mediaType,
			"size", descriptor.size,
			"kind", descriptor.kind,
			"error", err,
		)
	}
}

func cachedManifestMediaType(manifest *cache.CachedManifest) string {
	if manifest == nil {
		return ""
	}
	mediaType := normalizeManifestMediaType(manifest.MediaType)
	if mediaType != "" {
		return mediaType
	}
	return manifestBodyMediaType(manifest.Body)
}

func manifestBody(manifest *cache.CachedManifest) []byte {
	if manifest == nil {
		return nil
	}
	return manifest.Body
}

func isImageManifestMediaType(mediaType string) bool {
	switch normalizeManifestMediaType(mediaType) {
	case distribution.MediaTypeOCIManifest, distribution.MediaTypeDockerManifest:
		return true
	default:
		return false
	}
}

func normalizeManifestMediaType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err == nil {
		return mediaType
	}
	return value
}

func (e *RegistryEndpoint) logSkippedManifestBlobFill(
	ctx context.Context,
	route reference.Route,
	manifestDigest string,
	mediaType string,
	reason string,
) {
	if e == nil {
		return
	}
	e.publishWorkerFillSkipped(ctx, route, "blob", reason)
	if e.logger == nil {
		return
	}
	e.logger.DebugContext(ctx, "skipped manifest blob fill",
		"alias", route.Alias,
		"repository", route.Repo,
		"reference", route.Reference,
		"digest", manifestDigest,
		"media_type", mediaType,
		"reason", reason,
	)
}

func (e *RegistryEndpoint) logSkippedManifestBlob(
	ctx context.Context,
	route reference.Route,
	manifestDigest string,
	descriptor manifestBlobDescriptor,
	reason string,
) {
	if e == nil {
		return
	}
	e.publishWorkerFillSkipped(ctx, route, "blob", reason)
	if e.logger == nil {
		return
	}
	e.logger.DebugContext(ctx, "skipped manifest blob",
		"alias", route.Alias,
		"repository", route.Repo,
		"reference", route.Reference,
		"manifest_digest", manifestDigest,
		"digest", descriptor.digest,
		"media_type", descriptor.mediaType,
		"size", descriptor.size,
		"kind", descriptor.kind,
		"reason", reason,
	)
}
