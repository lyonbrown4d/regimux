package container

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/containerd/platforms"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/samber/lo"
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

	selected := prewarmPlatformManifestDescriptors(route.Alias, children)
	if len(selected) == 0 {
		e.logSkippedManifestBlobFill(ctx, route, manifest, mediaType, "no configured platform child manifest")
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

func prewarmPlatformManifestDescriptors(alias string, descriptors []ocispec.Descriptor) []ocispec.Descriptor {
	policy := newIndexPrewarmPlatformPolicy(config.ActiveContainerPrewarmPlatforms(alias))
	return lo.Filter(descriptors, func(descriptor ocispec.Descriptor, _ int) bool {
		return usableManifestDescriptor(descriptor) && matchesPrewarmPlatform(policy, descriptor.Platform)
	})
}

func usableManifestDescriptor(descriptor ocispec.Descriptor) bool {
	return descriptor.Digest != "" && isImageManifestMediaType(descriptor.MediaType)
}

type indexPrewarmPlatformPolicy struct {
	all       bool
	platforms []ocispec.Platform
}

func newIndexPrewarmPlatformPolicy(values []string) indexPrewarmPlatformPolicy {
	policy := indexPrewarmPlatformPolicy{}
	if len(values) == 0 {
		values = []string{config.DefaultContainerPrewarmPlatform()}
	}
	for _, value := range values {
		platform, all, ok := parseIndexPrewarmPlatform(value)
		if all {
			policy.all = true
			policy.platforms = nil
			return policy
		}
		if !ok {
			continue
		}
		policy.platforms = append(policy.platforms, platform)
	}
	if !policy.all && len(policy.platforms) == 0 {
		policy.platforms = append(policy.platforms, defaultIndexPrewarmPlatform())
	}
	return policy
}

func parseIndexPrewarmPlatform(value string) (ocispec.Platform, bool, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ocispec.Platform{}, false, false
	}
	if value == config.ContainerPrewarmAllPlatforms {
		return ocispec.Platform{}, true, true
	}
	if !strings.Contains(value, "/") {
		return ocispec.Platform{}, false, false
	}
	platform, err := platforms.Parse(value)
	if err != nil {
		return ocispec.Platform{}, false, false
	}
	return platforms.Normalize(platform), false, true
}

func defaultIndexPrewarmPlatform() ocispec.Platform {
	platform, err := platforms.Parse(config.DefaultContainerPrewarmPlatform())
	if err != nil {
		return ocispec.Platform{OS: "linux", Architecture: "amd64"}
	}
	return platforms.Normalize(platform)
}

func matchesPrewarmPlatform(policy indexPrewarmPlatformPolicy, platform *ocispec.Platform) bool {
	if policy.all {
		return true
	}
	if platform == nil {
		return false
	}
	candidate := platforms.Normalize(*platform)
	return lo.SomeBy(policy.platforms, func(platform ocispec.Platform) bool {
		return platforms.OnlyStrict(platform).Match(candidate)
	})
}
