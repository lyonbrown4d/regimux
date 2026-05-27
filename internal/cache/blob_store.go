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
	resp, err := p.client.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		return wrapError(err, "fetch blob for storage")
	}
	if resp.Digest != "" && resp.Digest != req.Digest {
		if closeErr := closeHTTPBody(resp.Body, "blob storage body"); closeErr != nil {
			return closeErr
		}
		return distribution.ErrDigestMismatch.WithDetail(map[string]string{
			"expected": req.Digest,
			"actual":   resp.Digest,
		})
	}

	info, putErr := p.objects.Put(ctx, req.Digest, resp.Body, object.PutOptions{ContentType: contentTypeFromHeader(resp.Headers)})
	closeErr := closeHTTPBody(resp.Body, "blob storage body")
	if putErr != nil {
		if errors.Is(putErr, object.ErrDigestMismatch) {
			return distribution.ErrDigestMismatch.WithDetail(putErr.Error())
		}
		return wrapError(putErr, "store blob object")
	}
	if closeErr != nil {
		return closeErr
	}
	if err := p.upsertBlobRecords(ctx, req, info, contentTypeFromHeader(resp.Headers)); err != nil {
		return err
	}
	p.publishCacheStore(ctx, req, info.Size, info.Digest)
	return nil
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
