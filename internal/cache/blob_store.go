package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (p blobProxy) fetchAndStore(ctx context.Context, req BlobRequest) error {
	var stored storedBlob
	err := p.client.ConsumeBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Method:        http.MethodGet,
	}, func(resp *upstream.BlobResponse) error {
		result, storeErr := p.storeBlobResponse(ctx, req, resp)
		if storeErr != nil {
			return storeErr
		}
		stored = result
		return nil
	})
	if err != nil {
		return wrapError(err, "fetch blob for storage")
	}
	if stored.info == nil {
		return errorf("stored blob info is empty")
	}
	if err := p.upsertBlobRecords(ctx, req, stored.info, stored.mediaType); err != nil {
		return err
	}
	p.publishCacheStore(ctx, req, stored.info.Size, stored.info.Digest)
	return nil
}

type storedBlob struct {
	info      *object.Info
	mediaType string
}

func (p blobProxy) storeBlobResponse(ctx context.Context, req BlobRequest, resp *upstream.BlobResponse) (storedBlob, error) {
	if err := validateStoredBlobDigest(req.Digest, resp.Digest); err != nil {
		return storedBlob{}, err
	}
	mediaType := contentTypeFromHeader(resp.Headers)
	info, err := p.putBlobObject(ctx, req, resp, mediaType)
	if err != nil {
		return storedBlob{}, err
	}
	return storedBlob{info: info, mediaType: mediaType}, nil
}

func validateStoredBlobDigest(expected, actual string) error {
	if actual == "" || actual == expected {
		return nil
	}
	return distribution.ErrDigestMismatch.WithDetail(map[string]string{
		"expected": expected,
		"actual":   actual,
	})
}

func (p blobProxy) putBlobObject(ctx context.Context, req BlobRequest, resp *upstream.BlobResponse, mediaType string) (*object.Info, error) {
	recorder := p.newSmallBlobRecorder(resp)
	reader := smallBlobRecordingReader(resp.Body, recorder)
	info, err := p.objects.Put(ctx, req.Digest, reader, object.PutOptions{ContentType: mediaType})
	if err == nil {
		p.storeSmallBlobCache(ctx, req.Digest, mediaType, info.Size, recorder.Bytes(info.Size))
		return info, nil
	}
	if errors.Is(err, object.ErrDigestMismatch) {
		return nil, distribution.ErrDigestMismatch.WithDetail(err.Error())
	}
	return nil, wrapError(err, "store blob object")
}

func (p blobProxy) newSmallBlobRecorder(resp *upstream.BlobResponse) *smallBlobRecorder {
	if !p.smallCache.enabled || p.smallCache.maxSizeBytes <= 0 || resp == nil {
		return nil
	}
	if resp.Size > p.smallCache.maxSizeBytes {
		return nil
	}
	return newSmallBlobRecorder(p.smallCache.maxSizeBytes)
}

func (p blobProxy) upsertBlobRecords(ctx context.Context, req BlobRequest, info *object.Info, mediaType string) error {
	if p.metadata == nil || info == nil {
		return nil
	}
	now := time.Now().UTC()
	if _, err := p.metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       info.Digest,
		Size:         info.Size,
		MediaType:    mediaType,
		ObjectKey:    info.Digest,
		LastAccessAt: now,
	}); err != nil {
		return wrapError(err, "upsert blob record")
	}
	_, err := p.metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:          req.UpstreamAlias,
		Repository:     req.Repo,
		Digest:         info.Digest,
		LastAccessAt:   now,
		LastVerifiedAt: now,
	})
	if err != nil {
		return wrapError(err, "upsert repository blob record")
	}
	return nil
}

func (p blobProxy) openStored(ctx context.Context, req BlobRequest, cacheStatus CacheStatus) (*BlobReadResult, error) {
	p.logBlobLookupSkip(ctx, req, "read_cached_blob")
	info, err := p.objects.Stat(ctx, req.Digest)
	if err != nil {
		return nil, wrapError(err, "stat stored blob object")
	}
	storedInfo := *info
	headers := blobHeaders(info)

	status, size, opts, err := blobReadOptions(req, info.Size, headers)
	if err != nil {
		return nil, err
	}
	reader := io.NopCloser(bytes.NewReader(nil))
	if req.Method != http.MethodHead {
		reader, info, err = p.objects.Get(ctx, req.Digest, opts)
		if err != nil {
			return nil, wrapError(err, "open stored blob object")
		}
		size = info.Size
	}
	if err := p.touchStoredBlobAccess(ctx, req, &storedInfo); err != nil {
		if closeErr := reader.Close(); closeErr != nil {
			return nil, errors.Join(err, wrapError(closeErr, "close stored blob after access touch failure"))
		}
		return nil, err
	}
	return &BlobReadResult{
		Reader:  reader,
		Digest:  info.Digest,
		Size:    size,
		Range:   req.Range,
		Status:  status,
		Headers: headers,
		Cache:   cacheStatus,
	}, nil
}

func (p blobProxy) touchStoredBlobAccess(ctx context.Context, req BlobRequest, info *object.Info) error {
	if p.metadata == nil || info == nil {
		return nil
	}
	now := time.Now().UTC()
	blob, ok, err := p.metadata.Blob(ctx, meta.BlobKey{Digest: info.Digest})
	if err != nil {
		return wrapError(err, "lookup blob metadata for access touch")
	}
	if ok {
		blob.LastAccessAt = now
	} else {
		blob = &meta.BlobRecord{
			Digest:       info.Digest,
			Size:         info.Size,
			MediaType:    info.ContentType,
			ObjectKey:    info.Digest,
			LastAccessAt: now,
		}
	}
	_, err = p.metadata.UpsertBlob(ctx, *blob)
	if err != nil {
		return wrapError(err, "touch blob access metadata")
	}

	repoBlob, ok, err := p.metadata.RepoBlob(ctx, meta.RepoBlobKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     info.Digest,
	})
	if err != nil {
		return wrapError(err, "lookup repository blob metadata for access touch")
	}
	if !ok {
		return nil
	}
	repoBlob.LastAccessAt = now
	if _, err := p.metadata.UpsertRepoBlob(ctx, *repoBlob); err != nil {
		return wrapError(err, "touch repository blob access metadata")
	}
	return nil
}
