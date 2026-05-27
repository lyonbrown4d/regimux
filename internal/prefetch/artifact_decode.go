package prefetch

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func imageManifestBlobDescriptors(manifest *cache.CachedManifest) ([]blobDescriptor, int, error) {
	var payload ocispec.Manifest
	if err := json.Unmarshal(manifestBody(manifest), &payload); err != nil {
		return nil, 0, cacheWrap(err, "decode image manifest for prefetch")
	}

	descriptors := make([]blobDescriptor, 0, len(payload.Layers)+1)
	if payload.Config.Digest != "" {
		descriptors = append(descriptors, newBlobDescriptor(payload.Config, "config"))
	}
	for i := range payload.Layers {
		if payload.Layers[i].Digest == "" {
			descriptors = append(descriptors, blobDescriptor{kind: "layer"})
			continue
		}
		descriptors = append(descriptors, newBlobDescriptor(payload.Layers[i], "layer"))
	}
	return descriptors, len(payload.Layers), nil
}

func imageIndexDescriptors(manifest *cache.CachedManifest) ([]ocispec.Descriptor, error) {
	var payload ocispec.Index
	if err := json.Unmarshal(manifestBody(manifest), &payload); err != nil {
		return nil, cacheWrap(err, "decode manifest index for prefetch")
	}
	return payload.Manifests, nil
}

func closePrefetchBlob(result *cache.BlobReadResult) error {
	if result == nil || result.Reader == nil {
		return nil
	}
	if err := result.Reader.Close(); err != nil && !errors.Is(err, io.EOF) {
		return cacheWrap(err, "close prefetched blob")
	}
	return nil
}

func newBlobDescriptor(descriptor ocispec.Descriptor, kind string) blobDescriptor {
	return blobDescriptor{
		digest:    string(descriptor.Digest),
		mediaType: descriptor.MediaType,
		size:      descriptor.Size,
		kind:      kind,
	}
}

func usableManifestDescriptor(descriptor ocispec.Descriptor) bool {
	return descriptor.Digest != "" && isImageManifestMediaType(normalizeMediaType(descriptor.MediaType))
}

func cachedManifestDigest(manifest *cache.CachedManifest, fallback string) string {
	if manifest != nil && manifest.Digest != "" {
		return manifest.Digest
	}
	return fallback
}

func cachedManifestMediaType(manifest *cache.CachedManifest) string {
	if manifest == nil {
		return ""
	}
	mediaType := normalizeMediaType(manifest.MediaType)
	if mediaType != "" {
		return mediaType
	}
	var envelope manifestMediaEnvelope
	if err := json.Unmarshal(manifest.Body, &envelope); err != nil {
		return ""
	}
	return normalizeMediaType(envelope.MediaType)
}

func manifestBody(manifest *cache.CachedManifest) []byte {
	if manifest == nil {
		return nil
	}
	return manifest.Body
}

func isImageManifestMediaType(mediaType string) bool {
	switch normalizeMediaType(mediaType) {
	case distribution.MediaTypeOCIManifest, distribution.MediaTypeDockerManifest:
		return true
	default:
		return false
	}
}

func isIndexManifestMediaType(mediaType string) bool {
	switch normalizeMediaType(mediaType) {
	case distribution.MediaTypeOCIIndex, distribution.MediaTypeDockerManifestList:
		return true
	default:
		return false
	}
}

func normalizeMediaType(value string) string {
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
