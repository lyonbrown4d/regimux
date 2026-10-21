package container

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/containerd/platforms"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/samber/lo"
)

type manifestChildDescriptor struct {
	digest    string
	mediaType string
	size      int64
}

func (e *RegistryEndpoint) fillManifestBlobsForIndex(
	ctx context.Context,
	route reference.Route,
	parentDigest string,
	children []manifestChildDescriptor,
) {
	if e.manifests == nil {
		return
	}
	for i := range children {
		e.fillChildManifestBlobs(ctx, route, parentDigest, children[i])
	}
}

func (e *RegistryEndpoint) fillChildManifestBlobs(
	ctx context.Context,
	route reference.Route,
	parentDigest string,
	child manifestChildDescriptor,
) {
	if child.digest == "" {
		return
	}
	descriptors, childMediaType, err := e.childManifestBlobDescriptors(ctx, route, child)
	if err != nil {
		e.logger.WarnContext(ctx, "index child manifest fill failed",
			"alias", route.Alias,
			"repository", route.Repo,
			"reference", route.Reference,
			"manifest_digest", parentDigest,
			"child_digest", child.digest,
			"media_type", child.mediaType,
			"error", err,
		)
		return
	}
	if !isImageManifestMediaType(childMediaType) {
		e.logSkippedManifestBlobFill(ctx, route, child.digest, childMediaType, "unsupported child manifest media type")
		return
	}
	childRoute := route
	childRoute.Reference = child.digest
	e.fillManifestBlobs(ctx, childRoute, child.digest, descriptors)
}

func (e *RegistryEndpoint) childManifestBlobDescriptors(
	ctx context.Context,
	route reference.Route,
	child manifestChildDescriptor,
) ([]manifestBlobDescriptor, string, error) {
	if err := e.metadataGate.Acquire(ctx, 1); err != nil {
		return nil, "", fmt.Errorf("acquire metadata admission: %w", err)
	}
	defer e.metadataGate.Release(1)

	manifest, err := e.manifests.Get(ctx, cache.ManifestRequest{
		UpstreamAlias:  route.Alias,
		Repo:           route.Repo,
		Reference:      child.digest,
		Accept:         child.mediaType,
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("get child manifest: %w", err)
	}
	if manifest == nil {
		return nil, "", errors.New("child manifest response is empty")
	}
	mediaType := cachedManifestMediaType(manifest)
	if !isImageManifestMediaType(mediaType) {
		return nil, mediaType, nil
	}
	descriptors, err := imageManifestBlobDescriptors(manifest)
	return descriptors, mediaType, err
}

func imageIndexManifestDescriptors(manifest *cache.CachedManifest) ([]ocispec.Descriptor, error) {
	if err := jsonbudget.Check(manifestBody(manifest), jsonbudget.DefaultLimits()); err != nil {
		return nil, wrapError(err, "check image index JSON budget")
	}
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

func prewarmManifestChildren(alias string, manifest *cache.CachedManifest) ([]manifestChildDescriptor, error) {
	descriptors, err := imageIndexManifestDescriptors(manifest)
	if err != nil {
		return nil, err
	}
	policy := newIndexPrewarmPlatformPolicy(config.ActiveContainerPrewarmPlatforms(alias))
	selected := lo.Filter(descriptors, func(descriptor ocispec.Descriptor, _ int) bool {
		return usableManifestDescriptor(descriptor) && matchesPrewarmPlatform(policy, descriptor.Platform)
	})
	children := make([]manifestChildDescriptor, len(selected))
	for i := range selected {
		children[i] = manifestChildDescriptor{
			digest:    string(selected[i].Digest),
			mediaType: selected[i].MediaType,
			size:      selected[i].Size,
		}
	}
	return children, nil
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
