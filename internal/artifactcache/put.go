package artifactcache

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func (s *Store) Put(ctx context.Context, req PutRequest) (Entry, error) {
	if err := s.validatePutRequest(req); err != nil {
		return Entry{}, err
	}
	tmp, tmpName, err := createArtifactTemp()
	if err != nil {
		return Entry{}, err
	}
	defer removePath(tmpName, s.logger)

	digest, size, err := hashToTemp(tmp, tmpName, req.Body)
	if err != nil {
		return Entry{}, err
	}
	validationErr := ValidateBody(tmp, size, req.Headers, req.Validator)
	if validationErr != nil {
		return Entry{}, closeAndRemoveTemp(tmp, tmpName, validationErr, "validate artifact body")
	}

	headers := cacheHeaders(req.Headers, size)
	info, err := s.objects.Put(ctx, digest, tmp, object.PutOptions{ContentType: req.ContentType})
	if err != nil {
		return Entry{}, closeAndRemoveTemp(tmp, tmpName, err, "store artifact object")
	}
	if closeErr := closeArtifactTemp(tmp, tmpName); closeErr != nil {
		return Entry{}, closeErr
	}
	if metadataErr := s.storeMetadata(ctx, req, digest, info, headers); metadataErr != nil {
		return Entry{}, metadataErr
	}
	reader, _, err := s.objects.Get(ctx, digest, object.GetOptions{})
	if err != nil {
		return Entry{}, wrapError(err, "open stored artifact object")
	}
	return Entry{
		Digest:  digest,
		Size:    size,
		Headers: headers,
		Body:    reader,
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

func createArtifactTemp() (*os.File, string, error) {
	tmp, err := os.CreateTemp("", "regimux-artifact-cache-*")
	if err != nil {
		return nil, "", wrapError(err, "create artifact temp file")
	}
	name := tmp.Name()
	return tmp, name, nil
}

func closeArtifactTemp(tmp *os.File, tmpName string) error {
	if closeErr := tmp.Close(); closeErr != nil {
		removeErr := os.Remove(tmpName)
		return wrapError(errors.Join(closeErr, removeErr), "close artifact temp file")
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
