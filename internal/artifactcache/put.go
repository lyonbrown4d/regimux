package artifactcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/spool"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func (s *Store) Put(ctx context.Context, req PutRequest) (entry Entry, err error) {
	if validationErr := s.validatePutRequest(req); validationErr != nil {
		return Entry{}, validationErr
	}
	tmp, err := spool.NewTempBody("regimux-artifact-cache-*")
	if err != nil {
		return Entry{}, wrapError(err, "create artifact temporary body")
	}
	transferred := false
	defer func() {
		if !transferred {
			err = errors.Join(err, tmp.Close())
		}
	}()
	entry, err = s.persistPutBody(ctx, req, tmp)
	// The caller owns the verified spool after persistence succeeds.
	transferred = err == nil
	return entry, err
}

func (s *Store) persistPutBody(ctx context.Context, req PutRequest, tmp *spool.TempBody) (Entry, error) {
	digest, size, err := hashToTemp(tmp, req.Body)
	if err != nil {
		return Entry{}, err
	}
	validationErr := ValidateBody(tmp, size, req.Headers, req.Validator)
	if validationErr != nil {
		return Entry{}, wrapError(validationErr, "validate artifact body")
	}

	headers := cacheHeaders(req.Headers, size)
	info, err := s.objects.Put(ctx, digest, object.ImmutableFileReader(tmp.File), object.PutOptions{ContentType: req.ContentType})
	if err != nil {
		return Entry{}, wrapError(err, "store artifact object")
	}
	if info != nil && info.Size != size {
		return Entry{}, fmt.Errorf("stored artifact object size %d does not match verified size %d", info.Size, size)
	}
	if metadataErr := s.storeMetadata(ctx, req, digest, info, headers); metadataErr != nil {
		return Entry{}, metadataErr
	}
	if _, seekErr := tmp.Seek(0, io.SeekStart); seekErr != nil {
		return Entry{}, wrapError(seekErr, "rewind stored artifact body")
	}
	return Entry{
		Digest:  digest,
		Size:    size,
		Headers: headers,
		Body:    tmp,
	}, nil
}

func (s *Store) validatePutRequest(req PutRequest) error {
	if s == nil || s.metadata == nil || s.objects == nil {
		return wrapError(errStoreNotConfigured, "artifact cache store is not configured")
	}
	if req.Body == nil {
		return wrapError(errEmptyBody, "artifact body is empty")
	}
	return nil
}

func (s *Store) storeMetadata(
	ctx context.Context,
	req PutRequest,
	digest string,
	info *object.Info,
	headers http.Header,
) error {
	if info == nil {
		return nil
	}
	// Reads starting after this write must not join an older metadata snapshot.
	// Invalidate on errors too: manifest or tag updates may already be committed.
	defer ForgetMetadata(s, req.Key)
	now := s.now()
	expiresAt := time.Time{}
	if req.TTL > 0 {
		expiresAt = now.Add(req.TTL)
	}
	if _, err := s.metadata.UpsertManifest(ctx, meta.ManifestRecord{
		Alias:      req.Key.Alias,
		Repository: req.Key.Repository,
		Reference:  req.Key.Reference,
		AcceptKey:  req.AcceptKey,
		Digest:     digest,
		MediaType:  req.ContentType,
		Size:       info.Size,
		ObjectKey:  digest,
		Headers:    map[string][]string(headers.Clone()),
		ExpiresAt:  expiresAt,
	}); err != nil {
		return wrapError(err, "upsert artifact manifest metadata")
	}
	if _, err := s.metadata.UpsertTag(ctx, meta.TagRecord{
		Alias:      req.Key.Alias,
		Repository: req.Key.Repository,
		Reference:  req.Key.Reference,
		Digest:     digest,
		ExpiresAt:  expiresAt,
	}); err != nil {
		return wrapError(err, "upsert artifact tag metadata")
	}
	return s.storeBlobMetadata(ctx, req, digest, info, now)
}

func (s *Store) storeBlobMetadata(ctx context.Context, req PutRequest, digest string, info *object.Info, now time.Time) error {
	if _, err := s.metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       digest,
		Size:         info.Size,
		MediaType:    req.ContentType,
		ObjectKey:    digest,
		LastAccessAt: now,
	}); err != nil {
		return wrapError(err, "upsert artifact blob metadata")
	}
	if _, err := s.metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:          req.Key.Alias,
		Repository:     req.Key.Repository,
		Digest:         digest,
		SourceManifest: digest,
		LastAccessAt:   now,
		LastVerifiedAt: now,
	}); err != nil {
		return wrapError(err, "upsert artifact repository blob metadata")
	}
	return nil
}
