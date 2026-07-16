package golang

import (
	"context"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/samber/oops"
)

type preparedGoProxyArtifact struct {
	file   *os.File
	path   string
	digest string
	size   int64
}

func (s *Service) store(ctx context.Context, requestRoute route, fetched *upstreamFetch) (storedResponse, error) {
	if fetched == nil || fetched.body == nil {
		return storedResponse{}, oops.In("go").Errorf("go proxy upstream body is empty")
	}
	defer closeReadCloser(fetched.body, s.logger, "close go proxy upstream body")

	prepared, err := s.prepareGoProxyArtifact(requestRoute, fetched)
	if err != nil {
		return storedResponse{}, err
	}
	defer removePath(prepared.path, s.logger)

	headers := cacheHeaders(fetched.headers, prepared.size)
	contentType := contentType(headers, requestRoute.Reference)
	info, err := s.objects.Put(
		ctx,
		prepared.digest,
		prepared.file,
		object.PutOptions{ContentType: contentType},
	)
	closeErr := prepared.file.Close()
	if err != nil {
		return storedResponse{}, wrapError(err, "store go proxy object")
	}
	if closeErr != nil {
		return storedResponse{}, wrapError(closeErr, "close go proxy temp file")
	}

	if metadataErr := s.storeMetadata(
		ctx,
		requestRoute,
		prepared.digest,
		info,
		headers,
		contentType,
	); metadataErr != nil {
		return storedResponse{}, metadataErr
	}
	reader, _, err := s.objects.Get(ctx, prepared.digest, object.GetOptions{})
	if err != nil {
		return storedResponse{}, wrapError(err, "open stored go proxy object")
	}
	return storedResponse{
		digest:  prepared.digest,
		size:    prepared.size,
		headers: headers,
		body:    reader,
	}, nil
}

func (s *Service) prepareGoProxyArtifact(
	requestRoute route,
	fetched *upstreamFetch,
) (preparedGoProxyArtifact, error) {
	tmp, err := os.CreateTemp("", "regimux-go-*")
	if err != nil {
		return preparedGoProxyArtifact{}, wrapError(err, "create go proxy temp file")
	}
	tmpName := tmp.Name()
	digester := ocidigest.SHA256.Digester()
	size, err := io.Copy(io.MultiWriter(tmp, digester.Hash()), fetched.body)
	if err != nil {
		return preparedGoProxyArtifact{}, closeAndRemoveTemp(
			tmp,
			tmpName,
			err,
			"write go proxy temp file",
		)
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return preparedGoProxyArtifact{}, closeAndRemoveTemp(
			tmp,
			tmpName,
			err,
			"rewind go proxy temp file",
		)
	}
	if validationErr := validateGoProxyBody(
		requestRoute,
		tmp,
		size,
		fetched.headers,
	); validationErr != nil {
		return preparedGoProxyArtifact{}, closeAndRemoveTemp(
			tmp,
			tmpName,
			validationErr,
			"discard invalid go proxy temp file",
		)
	}
	return preparedGoProxyArtifact{
		file:   tmp,
		path:   tmpName,
		digest: digester.Digest().String(),
		size:   size,
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
		AcceptKey:  "go",
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
