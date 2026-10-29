package cachegc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
)

// RestoreOCIManifest repairs a missing CAS object without changing metadata or
// activity timestamps. Only bounded, digest-verified, traversable bodies qualify.
// The caller owns and closes body.
func (s *Service) RestoreOCIManifest(ctx context.Context, manifest meta.ManifestRecord, body io.Reader) error {
	if err := validateOCIReferenceRequest(ctx, s, manifest); err != nil {
		return err
	}
	if manifest.ObjectKey != "" && manifest.ObjectKey != manifest.Digest {
		return fmt.Errorf("cannot restore OCI manifest with noncanonical object key %q", manifest.ObjectKey)
	}
	data, err := io.ReadAll(io.LimitReader(body, manifestReadLimit+1))
	if err != nil {
		return fmt.Errorf("read replacement OCI manifest: %w", err)
	}
	if len(data) > manifestReadLimit {
		return errors.New("replacement OCI manifest exceeds cleanup read bound")
	}
	if err := validateReplacementManifest(manifest, data); err != nil {
		return err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return fmt.Errorf("restore OCI manifest context: %w", contextErr)
	}
	if _, err := s.objects.Put(ctx, manifest.Digest, bytes.NewReader(data), object.PutOptions{ContentType: manifest.MediaType}); err != nil {
		return fmt.Errorf("restore OCI manifest object: %w", err)
	}
	return nil
}

func validateReplacementManifest(manifest meta.ManifestRecord, data []byte) error {
	digest := ocidigest.Digest(manifest.Digest)
	if digest.Algorithm().FromBytes(data) != digest {
		return fmt.Errorf("replacement OCI manifest: %w", object.ErrDigestMismatch)
	}
	if err := validateManifestBody(manifest, data); err != nil {
		return err
	}
	refs, err := manifestReferences(manifest.MediaType, data)
	if err != nil {
		return fmt.Errorf("parse replacement OCI manifest: %w", err)
	}
	return validateDescriptorDigests(manifest.MediaType, refs)
}
