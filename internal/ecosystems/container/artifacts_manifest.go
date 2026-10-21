package container

import (
	"encoding/json"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/samber/lo"
)

type manifestBlobDescriptor struct {
	digest    string
	mediaType string
	size      int64
	kind      string
}

func imageManifestBlobDescriptors(manifest *cache.CachedManifest) ([]manifestBlobDescriptor, error) {
	if err := jsonbudget.Check(manifestBody(manifest), jsonbudget.DefaultLimits()); err != nil {
		return nil, wrapError(err, "check image manifest JSON budget")
	}
	var payload ocispec.Manifest
	if err := json.Unmarshal(manifestBody(manifest), &payload); err != nil {
		return nil, wrapError(err, "decode image manifest")
	}

	descriptors := lo.Map(payload.Layers, func(layer ocispec.Descriptor, _ int) manifestBlobDescriptor {
		return newManifestBlobDescriptor(layer, "layer")
	})
	if payload.Config.Digest != "" {
		descriptors = append([]manifestBlobDescriptor{newManifestBlobDescriptor(payload.Config, "config")}, descriptors...)
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
