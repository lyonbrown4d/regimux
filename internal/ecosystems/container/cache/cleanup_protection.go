package cache

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const cleanupManifestProtectionReadLimit = 4 << 20

var errCleanupManifestTooLarge = errors.New("manifest object too large for cleanup protection")

func (s *CleanupService) protectedBlobDigests(ctx context.Context) (*collectionset.Set[string], error) {
	manifests, err := s.metadata.ListManifests(ctx)
	if err != nil {
		return nil, wrapError(err, "list manifest metadata for cleanup")
	}

	if manifests == nil {
		return collectionset.NewSet[string](), nil
	}
	protected := collectionset.NewSetWithCapacity[string](manifests.Len() * 4)
	manifests.Range(func(_ int, manifest meta.ManifestRecord) bool {
		s.protectManifestReferences(ctx, protected, manifest)
		return true
	})
	return protected, nil
}

func (s *CleanupService) protectManifestReferences(
	ctx context.Context,
	protected *collectionset.Set[string],
	manifest meta.ManifestRecord,
) {
	addCleanupProtectedDigest(protected, manifest.Digest)
	addCleanupProtectedDigest(protected, manifest.ObjectKey)

	body, err := s.readCleanupManifestBody(ctx, manifest)
	if err != nil {
		s.logCleanupManifestProtectionError(ctx, manifest, err)
		return
	}
	addCleanupManifestReferenceDigests(protected, manifest.MediaType, body)
}

func (s *CleanupService) readCleanupManifestBody(ctx context.Context, manifest meta.ManifestRecord) ([]byte, error) {
	objectKey := cleanupManifestObjectKey(manifest)
	if objectKey == "" || s.objects == nil {
		return nil, nil
	}
	reader, _, err := s.objects.Get(ctx, objectKey, object.GetOptions{})
	if err != nil {
		if errors.Is(err, object.ErrNotFound) {
			return nil, nil
		}
		return nil, wrapError(err, "read manifest object for cleanup protection")
	}
	if reader == nil {
		return nil, nil
	}
	body, readErr := io.ReadAll(io.LimitReader(reader, cleanupManifestProtectionReadLimit+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, wrapError(readErr, "read manifest object for cleanup protection")
	}
	if closeErr != nil {
		return nil, wrapError(closeErr, "close manifest object for cleanup protection")
	}
	if len(body) > cleanupManifestProtectionReadLimit {
		return nil, errCleanupManifestTooLarge
	}
	return body, nil
}

func cleanupManifestObjectKey(manifest meta.ManifestRecord) string {
	if manifest.ObjectKey != "" {
		return manifest.ObjectKey
	}
	return manifest.Digest
}

func addCleanupManifestReferenceDigests(
	protected *collectionset.Set[string],
	mediaType string,
	body []byte,
) {
	if len(body) == 0 {
		return
	}
	switch cleanupManifestMediaType(mediaType, body) {
	case distribution.MediaTypeOCIManifest, distribution.MediaTypeDockerManifest:
		addCleanupImageManifestDigests(protected, body)
	case distribution.MediaTypeOCIIndex, distribution.MediaTypeDockerManifestList:
		addCleanupImageIndexDigests(protected, body)
	}
}

func cleanupManifestMediaType(mediaType string, body []byte) string {
	mediaType = distribution.NormalizeMediaType(mediaType)
	if mediaType != "" {
		return mediaType
	}
	var envelope struct {
		MediaType string
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	return distribution.NormalizeMediaType(envelope.MediaType)
}
func addCleanupImageManifestDigests(protected *collectionset.Set[string], body []byte) {
	var manifest ocispec.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return
	}
	addCleanupDescriptorDigest(protected, manifest.Subject)
	addCleanupDescriptorDigest(protected, &manifest.Config)
	for i := range manifest.Layers {
		addCleanupDescriptorDigest(protected, &manifest.Layers[i])
	}
}

func addCleanupImageIndexDigests(protected *collectionset.Set[string], body []byte) {
	var index ocispec.Index
	if err := json.Unmarshal(body, &index); err != nil {
		return
	}
	addCleanupDescriptorDigest(protected, index.Subject)
	for i := range index.Manifests {
		addCleanupDescriptorDigest(protected, &index.Manifests[i])
	}
}

func addCleanupDescriptorDigest(
	protected *collectionset.Set[string],
	descriptor *ocispec.Descriptor,
) {
	if descriptor == nil {
		return
	}
	addCleanupProtectedDigest(protected, string(descriptor.Digest))
}
func addCleanupProtectedDigest(protected *collectionset.Set[string], digest string) {
	if protected == nil || digest == "" {
		return
	}
	protected.Add(digest)
}

func (s *CleanupService) logCleanupManifestProtectionError(
	ctx context.Context,
	manifest meta.ManifestRecord,
	err error,
) {
	if s == nil || s.logger == nil || err == nil {
		return
	}
	s.logger.WarnContext(ctx,
		"cleanup manifest protection skipped",
		"alias", manifest.Alias,
		"repository", manifest.Repository,
		"digest", manifest.Digest,
		"media_type", manifest.MediaType,
		"error", err,
	)
}
