package container

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func (e *RegistryEndpoint) fillManifestBlobsForIndex(
	ctx context.Context,
	route reference.Route,
	manifest *cache.CachedManifest,
	mediaType string,
) {
	if e.manifests == nil {
		return
	}
	children, err := imageIndexManifestDescriptors(manifest)
	if err != nil {
		e.logger.WarnContext(ctx, "decode image index for blob fill failed",
			"alias", route.Alias,
			"repository", route.Repo,
			"reference", route.Reference,
			"digest", manifest.Digest,
			"media_type", mediaType,
			"error", err,
		)
		return
	}

	selected := currentPlatformManifestDescriptors(children)
	if len(selected) == 0 {
		e.logSkippedManifestBlobFill(ctx, route, manifest, mediaType, "no current platform child manifest")
		return
	}

	for i := range selected {
		e.fillChildManifestBlobs(ctx, route, manifest, selected[i])
	}
}

func (e *RegistryEndpoint) fillChildManifestBlobs(
	ctx context.Context,
	route reference.Route,
	parent *cache.CachedManifest,
	child ocispec.Descriptor,
) {
	childDigest := string(child.Digest)
	if childDigest == "" {
		return
	}
	childManifest, err := e.manifests.Get(ctx, cache.ManifestRequest{
		UpstreamAlias:  route.Alias,
		Repo:           route.Repo,
		Reference:      childDigest,
		Accept:         child.MediaType,
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		e.logger.WarnContext(ctx, "index child manifest fill failed",
			"alias", route.Alias,
			"repository", route.Repo,
			"reference", route.Reference,
			"manifest_digest", parent.Digest,
			"child_digest", childDigest,
			"media_type", child.MediaType,
			"error", err,
		)
		return
	}

	childMediaType := cachedManifestMediaType(childManifest)
	if !isImageManifestMediaType(childMediaType) {
		e.logSkippedManifestBlobFill(ctx, route, childManifest, childMediaType, "unsupported child manifest media type")
		return
	}
	childRoute := route
	childRoute.Reference = childDigest
	e.fillManifestBlobsForManifest(ctx, childRoute, childManifest, childMediaType)
}

func imageIndexManifestDescriptors(manifest *cache.CachedManifest) ([]ocispec.Descriptor, error) {
	var payload ocispec.Index
	if err := json.Unmarshal(manifestBody(manifest), &payload); err != nil {
		return nil, wrapError(err, "decode image index")
	}
	return payload.Manifests, nil
}

func isIndexManifestMediaType(mediaType string) bool {
	switch normalizeManifestMediaType(mediaType) {
	case distribution.MediaTypeOCIIndex, distribution.MediaTypeDockerManifestList:
		return true
	default:
		return false
	}
}

func currentPlatformManifestDescriptors(descriptors []ocispec.Descriptor) []ocispec.Descriptor {
	out := make([]ocispec.Descriptor, 0, len(descriptors))
	for i := range descriptors {
		descriptor := descriptors[i]
		if !usableManifestDescriptor(descriptor) || !matchesCurrentPlatform(descriptor.Platform) {
			continue
		}
		out = append(out, descriptor)
	}
	return out
}

func usableManifestDescriptor(descriptor ocispec.Descriptor) bool {
	return descriptor.Digest != "" && isImageManifestMediaType(descriptor.MediaType)
}

func matchesCurrentPlatform(platform *ocispec.Platform) bool {
	if platform == nil {
		return false
	}
	if !strings.EqualFold(platform.OS, runtime.GOOS) || !strings.EqualFold(platform.Architecture, runtime.GOARCH) {
		return false
	}
	return true
}
