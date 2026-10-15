package artifactcache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const accessTouchTimeout = 500 * time.Millisecond

// Touch records a successful cache access for the shared content-addressed blob.
// The timeout keeps metadata writes from delaying artifact delivery.
func Touch(ctx context.Context, metadata meta.BlobRepository, manifest meta.ManifestRecord, at time.Time) error {
	if err := validateTouch(ctx, metadata, manifest.Digest); err != nil {
		return err
	}
	touchCtx, cancel := context.WithTimeout(ctx, accessTouchTimeout)
	defer cancel()
	return touchBlob(touchCtx, metadata, manifest, normalizeTouchTime(at))
}

func validateTouch(ctx context.Context, metadata meta.BlobRepository, digest string) error {
	if ctx == nil {
		return errors.New("touch artifact access: context is required")
	}
	if metadata == nil {
		return errors.New("touch artifact access: metadata store is required")
	}
	if digest == "" {
		return errors.New("touch artifact access: digest is required")
	}
	return nil
}

func normalizeTouchTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}

func touchBlob(ctx context.Context, metadata meta.BlobRepository, manifest meta.ManifestRecord, at time.Time) error {
	record, found, err := metadata.Blob(ctx, meta.BlobKey{Digest: manifest.Digest})
	if err != nil {
		return fmt.Errorf("read artifact access metadata for %s: %w", manifest.Digest, err)
	}
	if found && !at.After(record.LastAccessAt) {
		return nil
	}
	if found {
		if accessStore, ok := metadata.(meta.BlobAccessRepository); ok {
			return touchBlobAccess(ctx, accessStore, manifest.Digest, at)
		}
		record.LastAccessAt = at
		_, err = metadata.UpsertBlob(ctx, *record)
		return wrapTouchError(manifest.Digest, err)
	}
	return seedBlobAccess(ctx, metadata, manifest, at)
}

func touchBlobAccess(ctx context.Context, store meta.BlobAccessRepository, digest string, at time.Time) error {
	if err := store.TouchBlob(ctx, meta.BlobKey{Digest: digest}, at); err != nil {
		return fmt.Errorf("touch artifact access for %s: %w", digest, err)
	}
	return nil
}

func seedBlobAccess(ctx context.Context, metadata meta.BlobRepository, manifest meta.ManifestRecord, at time.Time) error {
	objectKey := manifest.ObjectKey
	if objectKey == "" {
		objectKey = manifest.Digest
	}
	_, err := metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest: manifest.Digest, Size: manifest.Size, MediaType: manifest.MediaType,
		ObjectKey: objectKey, LastAccessAt: at,
	})
	return wrapTouchError(manifest.Digest, err)
}

func wrapTouchError(digest string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("touch artifact access for %s: %w", digest, err)
}
