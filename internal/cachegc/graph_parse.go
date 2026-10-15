package cachegc

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func manifestReferences(mediaType string, body []byte) ([]string, error) {
	mediaType = distribution.NormalizeMediaType(mediaType)
	if mediaType == "" {
		var envelope struct {
			MediaType string `json:"mediaType"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			// Opaque package files may be stored without a media type.
			return nil, nil //nolint:nilerr // Non-JSON opaque cache payloads are not OCI manifests.
		}
		mediaType = distribution.NormalizeMediaType(envelope.MediaType)
	}
	if len(body) == 0 && isOCIManifestType(mediaType) {
		return nil, errors.New("manifest object is unavailable")
	}
	if len(body) == 0 || !isOCIManifestType(mediaType) {
		return nil, nil
	}
	return decodeManifestReferences(mediaType, body)
}

func decodeManifestReferences(mediaType string, body []byte) ([]string, error) {
	if mediaType == distribution.MediaTypeOCIManifest || mediaType == distribution.MediaTypeDockerManifest {
		return decodeOCIManifest(body)
	}
	return decodeOCIIndex(body)
}

func decodeOCIManifest(body []byte) ([]string, error) {
	var manifest ocispec.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("decode OCI manifest: %w", err)
	}
	refs := make([]string, 0, len(manifest.Layers)+2)
	if manifest.Subject != nil {
		refs = append(refs, string(manifest.Subject.Digest))
	}
	refs = append(refs, string(manifest.Config.Digest))
	for i := range manifest.Layers {
		refs = append(refs, string(manifest.Layers[i].Digest))
	}
	return refs, nil
}

func decodeOCIIndex(body []byte) ([]string, error) {
	var index ocispec.Index
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("decode OCI index: %w", err)
	}
	refs := make([]string, 0, len(index.Manifests)+1)
	if index.Subject != nil {
		refs = append(refs, string(index.Subject.Digest))
	}
	for i := range index.Manifests {
		refs = append(refs, string(index.Manifests[i].Digest))
	}
	return refs, nil
}

func isOCIManifestType(mediaType string) bool {
	switch distribution.NormalizeMediaType(mediaType) {
	case distribution.MediaTypeOCIManifest, distribution.MediaTypeDockerManifest,
		distribution.MediaTypeOCIIndex, distribution.MediaTypeDockerManifestList:
		return true
	default:
		return false
	}
}
