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
) (plan *manifestBlobFillPlan) {
	release := e.admitManifestBlobFill(ctx, route, manifest)
	if release == nil {
		return nil
	}
	defer func() {
		if plan == nil {
			release()
		}
	}()
	mediaType := cachedManifestMediaType(manifest)
	manifestDigest := manifest.Digest
	if isImageManifestMediaType(mediaType) {
		return e.prepareImageBlobFillPlan(ctx, route, manifest, mediaType, release)
	}
	if isIndexManifestMediaType(mediaType) {
		return e.prepareIndexBlobFillPlan(ctx, route, manifest, mediaType, release)
	}
	e.logSkippedManifestBlobFill(ctx, route, manifestDigest, mediaType, "unsupported manifest media type")
	return nil
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
