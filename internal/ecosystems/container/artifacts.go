package container

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type manifestBlobDescriptor struct {
	digest    string
	mediaType string
	size      int64
	kind      string
}

type manifestMediaEnvelope struct {
	MediaType string `json:"mediaType"`
}

func (e *RegistryEndpoint) fillManifestBlobsAsync(ctx context.Context, route reference.Route, manifest *cache.CachedManifest) {
	if e == nil || e.blobs == nil || manifest == nil {
		return
	}
	mediaType := cachedManifestMediaType(manifest)
	if !isImageManifestMediaType(mediaType) {
		e.logSkippedManifestBlobFill(ctx, route, manifest, mediaType, "unsupported manifest media type")
		return
	}

	go e.fillManifestBlobsForManifest(context.WithoutCancel(ctx), route, manifest, mediaType)
}

func (e *RegistryEndpoint) fillManifestBlobsForManifest(
	ctx context.Context,
	route reference.Route,
	manifest *cache.CachedManifest,
	mediaType string,
) {
	descriptors, err := imageManifestBlobDescriptors(manifest)
	if err != nil {
		e.logger.WarnContext(ctx, "decode image manifest for blob fill failed",
			"alias", route.Alias,
			"repository", route.Repo,
			"reference", route.Reference,
			"digest", manifest.Digest,
			"media_type", mediaType,
			"error", err,
		)
		return
	}
	if len(descriptors) == 0 {
		return
	}

	e.fillManifestBlobs(ctx, route, manifest.Digest, descriptors)
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

func imageManifestBlobDescriptors(manifest *cache.CachedManifest) ([]manifestBlobDescriptor, error) {
	var payload ocispec.Manifest
	if err := json.Unmarshal(manifestBody(manifest), &payload); err != nil {
		return nil, err
	}

	descriptors := make([]manifestBlobDescriptor, 0, len(payload.Layers)+1)
	if payload.Config.Digest != "" {
		descriptors = append(descriptors, newManifestBlobDescriptor(payload.Config, "config"))
	}
	for i := range payload.Layers {
		descriptors = append(descriptors, newManifestBlobDescriptor(payload.Layers[i], "layer"))
	}
	return descriptors, nil
}

func newManifestBlobDescriptor(descriptor ocispec.Descriptor, kind string) manifestBlobDescriptor {
	return manifestBlobDescriptor{
		digest:    string(descriptor.Digest),
		mediaType: descriptor.MediaType,
		size:      descriptor.Size,
		kind:      kind,
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
	var envelope manifestMediaEnvelope
	if err := json.Unmarshal(manifest.Body, &envelope); err != nil {
		return ""
	}
	return normalizeManifestMediaType(envelope.MediaType)
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
	manifest *cache.CachedManifest,
	mediaType string,
	reason string,
) {
	if e == nil || e.logger == nil {
		return
	}
	e.logger.DebugContext(ctx, "skipped manifest blob fill",
		"alias", route.Alias,
		"repository", route.Repo,
		"reference", route.Reference,
		"digest", manifest.Digest,
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
	if e == nil || e.logger == nil {
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
