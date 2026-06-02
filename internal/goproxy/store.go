package goproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/samber/oops"
)

func (s *Service) store(ctx context.Context, requestRoute route, fetched *upstreamFetch) (storedResponse, error) {
	if fetched == nil || fetched.body == nil {
		return storedResponse{}, oops.In("go-proxy").Errorf("go proxy upstream body is empty")
	}
	defer closeReadCloser(fetched.body, s.logger, "close go proxy upstream body")

	tmp, err := os.CreateTemp("", "regimux-go-proxy-*")
	if err != nil {
		return storedResponse{}, wrapError(err, "create go proxy temp file")
	}
	tmpName := tmp.Name()
	defer removePath(tmpName, s.logger)

	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), fetched.body)
	if err != nil {
		return storedResponse{}, closeAndRemoveTemp(tmp, tmpName, err, "write go proxy temp file")
	}
	_, seekErr := tmp.Seek(0, io.SeekStart)
	if seekErr != nil {
		return storedResponse{}, closeAndRemoveTemp(tmp, tmpName, seekErr, "rewind go proxy temp file")
	}

	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	headers := cacheHeaders(fetched.headers, size)
	contentType := contentType(headers, requestRoute.Reference)
	info, err := s.objects.Put(ctx, digest, tmp, object.PutOptions{ContentType: contentType})
	closeErr := tmp.Close()
	if err != nil {
		return storedResponse{}, wrapError(err, "store go proxy object")
	}
	if closeErr != nil {
		return storedResponse{}, wrapError(closeErr, "close go proxy temp file")
	}

	if metadataErr := s.storeMetadata(ctx, requestRoute, digest, info, headers, contentType); metadataErr != nil {
		return storedResponse{}, metadataErr
	}
	reader, _, err := s.objects.Get(ctx, digest, object.GetOptions{})
	if err != nil {
		return storedResponse{}, wrapError(err, "open stored go proxy object")
	}
	return storedResponse{
		digest:  digest,
		size:    size,
		headers: headers,
		body:    reader,
	}, nil
}

func (s *Service) storeMetadata(ctx context.Context, requestRoute route, digest string, info *object.Info, headers http.Header, contentType string) error {
	if s.metadata == nil || info == nil {
		return nil
	}
	now := time.Now().UTC()
	expiresAt := time.Time{}
	if ttl := routeMetadataTTL(requestRoute, defaultMetadataTTL); ttl > 0 {
		expiresAt = now.Add(ttl)
	}
	if err := s.storeContentMetadata(ctx, requestRoute, digest, info, headers, contentType, expiresAt); err != nil {
		return err
	}
	return s.storeBlobMetadata(ctx, requestRoute, digest, info, contentType, now)
}

func (s *Service) storeContentMetadata(ctx context.Context, requestRoute route, digest string, info *object.Info, headers http.Header, contentType string, expiresAt time.Time) error {
	record := meta.ManifestRecord{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Module,
		Reference:  requestRoute.Reference,
		AcceptKey:  "go-proxy",
		Digest:     digest,
		MediaType:  contentType,
		Size:       info.Size,
		ObjectKey:  digest,
		Headers:    map[string][]string(headers.Clone()),
		ExpiresAt:  expiresAt,
	}
	if _, err := s.metadata.UpsertManifest(ctx, record); err != nil {
		return wrapError(err, "upsert go proxy content metadata")
	}
	if _, err := s.metadata.UpsertTag(ctx, meta.TagRecord{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Module,
		Reference:  requestRoute.Reference,
		Digest:     digest,
		ExpiresAt:  expiresAt,
	}); err != nil {
		return wrapError(err, "upsert go proxy request metadata")
	}
	return nil
}

func (s *Service) storeBlobMetadata(ctx context.Context, requestRoute route, digest string, info *object.Info, contentType string, now time.Time) error {
	if _, err := s.metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       digest,
		Size:         info.Size,
		MediaType:    contentType,
		ObjectKey:    digest,
		LastAccessAt: now,
	}); err != nil {
		return wrapError(err, "upsert go proxy blob metadata")
	}
	if _, err := s.metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:          requestRoute.Alias,
		Repository:     requestRoute.Module,
		Digest:         digest,
		SourceManifest: digest,
		LastAccessAt:   now,
		LastVerifiedAt: now,
	}); err != nil {
		return wrapError(err, "upsert go proxy repository blob metadata")
	}
	return nil
}
