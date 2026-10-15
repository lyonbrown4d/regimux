package cachegc

import (
	"context"
	"errors"
	"fmt"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	ocidigest "github.com/opencontainers/go-digest"
)

// OCIReferences returns descriptors directly referenced by a cached OCI or
// Docker image manifest/index. It reads only the bounded local object and does
// not contact an upstream registry.
func (s *Service) OCIReferences(ctx context.Context, manifest meta.ManifestRecord) ([]string, error) {
	if err := validateOCIReferenceRequest(ctx, s, manifest); err != nil {
		if errors.Is(err, errNotOCIManifest) {
			return nil, nil
		}
		return nil, err
	}
	return s.readOCIReferences(ctx, manifest)
}

var errNotOCIManifest = errors.New("not an OCI manifest")

func validateOCIReferenceRequest(ctx context.Context, s *Service, manifest meta.ManifestRecord) error {
	if ctx == nil {
		return errors.New("OCI graph context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("OCI graph context: %w", err)
	}
	if !isOCIManifestType(manifest.MediaType) && manifest.MediaType != "" {
		return errNotOCIManifest
	}
	if s == nil || s.objects == nil {
		return errors.New("OCI graph object store is required")
	}
	if err := ocidigest.Digest(manifest.Digest).Validate(); err != nil {
		return fmt.Errorf("invalid OCI manifest digest %q: %w", manifest.Digest, err)
	}
	return nil
}

func (s *Service) readOCIReferences(ctx context.Context, manifest meta.ManifestRecord) ([]string, error) {
	body, readErr := s.readManifest(ctx, manifest)
	if readErr != nil {
		return nil, readErr
	}
	if bodyErr := validateManifestBody(manifest, body); bodyErr != nil {
		return nil, bodyErr
	}
	refs, parseErr := manifestReferences(manifest.MediaType, body)
	if parseErr != nil {
		return nil, fmt.Errorf("parse OCI manifest references: %w", parseErr)
	}
	if digestErr := validateDescriptorDigests(manifest.MediaType, refs); digestErr != nil {
		return nil, digestErr
	}
	return refs, nil
}

func validateManifestBody(manifest meta.ManifestRecord, body []byte) error {
	if len(body) == 0 && isOCIManifestType(manifest.MediaType) {
		return errors.New("OCI manifest object is unavailable")
	}
	if manifest.MediaType == "" && len(body) == 0 {
		return errors.New("untyped OCI manifest object is unavailable")
	}
	return nil
}

func validateDescriptorDigests(mediaType string, refs []string) error {
	if mediaType == "" && len(refs) == 0 {
		return errors.New("OCI manifest body has no recognized descriptors")
	}
	for _, ref := range refs {
		digest := ocidigest.Digest(ref)
		if err := digest.Validate(); err != nil {
			return fmt.Errorf("invalid OCI descriptor digest %q: %w", ref, err)
		}
	}
	return nil
}
