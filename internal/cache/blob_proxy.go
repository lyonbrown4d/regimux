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

func (p blobProxy) Get(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	if cached, ok, err := p.lookup(ctx, req); err != nil || ok {
		if cached != nil {
			p.publishCacheAccess(ctx, req, cached.Cache)
		}
		return cached, err
	}

	if p.shouldStreamAndCache(req) {
		result, err := p.fetchStreamAndStore(ctx, req)
		if result != nil {
			p.publishCacheAccess(ctx, req, result.Cache)
		}
		return result, err
	}

	if p.shouldBypassStore(req) {
		result, err := p.fetchPassthrough(ctx, req)
		if result != nil {
			p.publishCacheAccess(ctx, req, result.Cache)
		}
		return result, err
	}
	result, err := p.fetchStored(ctx, req)
	if result != nil {
		p.publishCacheAccess(ctx, req, result.Cache)
	}
	return result, err
}

func (p blobProxy) shouldBypassStore(req BlobRequest) bool {
	return p.metadata == nil || p.objects == nil || req.Method == http.MethodHead
}

func (p blobProxy) shouldStreamAndCache(req BlobRequest) bool {
	return p.streamAndCache &&
		req.Method != http.MethodHead &&
		req.Range != nil &&
		p.metadata != nil &&
		p.objects != nil
}

func (p blobProxy) fetchStored(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	key := "blob:" + req.Digest
	if p.group == nil {
		return p.fetchAndOpenStored(ctx, req)
	}

	_, err, _ := p.group.Do(key, func() (any, error) {
		return nil, p.ensureStored(ctx, req)
	})
	if err != nil {
		return nil, wrapError(err, "coalesce blob request")
	}
	return p.openStored(ctx, req, CacheMiss)
}

func (p blobProxy) fetchStreamAndStore(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	if req.Range == nil || req.Method == http.MethodHead {
		return nil, errorf("stream-and-cache range fetch requires range request")
	}
	resp, err := p.client.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Range:         req.Range,
		Method:        req.Method,
	})
	if err != nil {
		return nil, wrapError(err, "stream blob from upstream")
	}
	p.logBlobCacheHit(ctx, req, "stream_and_cache_range")
	reader := resp.Body
	if err := p.touchSharedBlobMetadata(ctx, req, time.Now().UTC()); err != nil {
		if closeErr := closeHTTPBody(resp.Body, "blob stream response body"); closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	return &BlobReadResult{
		Reader:  reader,
		Digest:  resp.Digest,
		Size:    resp.Size,
		Range:   req.Range,
		Status:  resp.StatusCode,
		Headers: resp.Headers,
		Cache:   CacheBypass,
	}, nil
}

func (p blobProxy) fetchAndOpenStored(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	if err := p.ensureStored(ctx, req); err != nil {
		return nil, err
	}
	return p.openStored(ctx, req, CacheMiss)
}

func (p blobProxy) ensureStored(ctx context.Context, req BlobRequest) error {
	if _, ok, err := p.lookup(ctx, BlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Method:        http.MethodHead,
	}); err != nil || ok {
		return err
	}
	return p.fetchAndStore(ctx, req)
}

func (p blobProxy) fetchPassthrough(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	resp, err := p.client.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Range:         req.Range,
		Method:        req.Method,
	})
	if err != nil {
		return nil, wrapError(err, "fetch blob from upstream")
	}

	reader := resp.Body
	if req.Method == http.MethodHead {
		if err := closeHTTPBody(resp.Body, "blob response body"); err != nil {
			return nil, err
		}
		reader = io.NopCloser(bytes.NewReader(nil))
	}
	return &BlobReadResult{
		Reader:  reader,
		Digest:  resp.Digest,
		Size:    resp.Size,
		Range:   req.Range,
		Status:  resp.StatusCode,
		Headers: resp.Headers,
		Cache:   CacheBypass,
	}, nil
}

func (p blobProxy) lookup(ctx context.Context, req BlobRequest) (*BlobReadResult, bool, error) {
	if p.metadata == nil || p.objects == nil {
		return nil, false, nil
	}
	if result, ok, err := p.lookupRepoBlob(ctx, req); err != nil || ok {
		return result, ok, err
	}
	return p.lookupSharedBlob(ctx, req)
}

func (p blobProxy) lookupRepoBlob(ctx context.Context, req BlobRequest) (*BlobReadResult, bool, error) {
	_, ok, err := p.metadata.RepoBlob(ctx, meta.RepoBlobKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     req.Digest,
	})
	if err != nil {
		return nil, false, wrapError(err, "lookup repository blob record")
	}
	if !ok {
		return nil, false, nil
	}
	shouldVerify, err := p.shouldVerifySharedBlob(ctx, req)
	if err != nil {
		return nil, false, err
	}
	if shouldVerify {
		if err := p.verifyRepoBlob(ctx, req); err != nil {
			return nil, false, err
		}
	}
	return p.openExistingStored(ctx, req, CacheHit)
}

func (p blobProxy) lookupSharedBlob(ctx context.Context, req BlobRequest) (*BlobReadResult, bool, error) {
	exists, err := p.objects.Exists(ctx, req.Digest)
	if err != nil {
		return nil, false, wrapError(err, "check blob object")
	}
	if !exists {
		return nil, false, nil
	}
	p.logBlobCacheHit(ctx, req, "shared_object_hit")
	shouldVerify, err := p.shouldVerifySharedBlob(ctx, req)
	if err != nil {
		return nil, false, err
	}
	if shouldVerify {
		if err := p.verifyRepoBlob(ctx, req); err != nil {
			return nil, false, err
		}
	}
	return p.openExistingStored(ctx, req, CacheHit)
}

func (p blobProxy) shouldVerifySharedBlob(ctx context.Context, req BlobRequest) (bool, error) {
	if p.verifyMembership <= 0 {
		return false, nil
	}
	if p.metadata == nil {
		return false, nil
	}
	record, ok, err := p.metadata.RepoBlob(ctx, meta.RepoBlobKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     req.Digest,
	})
	if err != nil {
		return false, wrapError(err, "lookup shared blob verification record")
	}
	if !ok {
		return true, nil
	}
	if record.LastVerifiedAt.IsZero() {
		return true, nil
	}
	if now := time.Now().UTC(); now.Sub(record.LastVerifiedAt) >= p.verifyMembership {
		return true, nil
	}
	p.logBlobCacheHit(ctx, req, "shared_object_verify_skipped")
	return false, nil
}

func (p blobProxy) touchSharedBlobMetadata(ctx context.Context, req BlobRequest, now time.Time) error {
	if p.metadata == nil {
		return nil
	}
	blob, ok, err := p.metadata.Blob(ctx, meta.BlobKey{Digest: req.Digest})
	if err != nil {
		return wrapError(err, "lookup shared blob metadata")
	}
	if !ok {
		return nil
	}
	blob.LastAccessAt = now
	if _, err := p.metadata.UpsertBlob(ctx, *blob); err != nil {
		return wrapError(err, "touch shared blob metadata")
	}
	repoBlob, ok, err := p.metadata.RepoBlob(ctx, meta.RepoBlobKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     req.Digest,
	})
	if err != nil {
		return wrapError(err, "lookup shared repo blob metadata")
	}
	if !ok {
		return nil
	}
	repoBlob.LastAccessAt = now
	repoBlob.LastVerifiedAt = now
	_, err = p.metadata.UpsertRepoBlob(ctx, *repoBlob)
	if err != nil {
		return wrapError(err, "touch shared repo blob metadata")
	}
	return nil
}

func (p blobProxy) logBlobCacheHit(ctx context.Context, req BlobRequest, reason string) {
	if p.logger == nil {
		return
	}
	p.logger.DebugContext(ctx,
		"blob cache event",
		"reason", reason,
		"alias", req.UpstreamAlias,
		"repo", req.Repo,
		"digest", req.Digest,
	)
}

func (p blobProxy) logBlobLookupSkip(ctx context.Context, req BlobRequest, reason string) {
	if p.logger == nil {
		return
	}
	p.logger.DebugContext(ctx,
		"blob cache lookup skipped",
		"reason", reason,
		"alias", req.UpstreamAlias,
		"repo", req.Repo,
		"digest", req.Digest,
	)
}

func (p blobProxy) openExistingStored(ctx context.Context, req BlobRequest, cacheStatus CacheStatus) (*BlobReadResult, bool, error) {
	result, err := p.openStored(ctx, req, cacheStatus)
	if errors.Is(err, object.ErrNotFound) {
		return nil, false, nil
	}
	return result, err == nil, err
}

func (p blobProxy) verifyRepoBlob(ctx context.Context, req BlobRequest) error {
	resp, err := p.client.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Method:        http.MethodHead,
	})
	if err != nil {
		return wrapError(err, "verify blob upstream membership")
	}
	if closeErr := closeHTTPBody(resp.Body, "blob verification body"); closeErr != nil {
		return closeErr
	}
	if resp.Digest != "" && resp.Digest != req.Digest {
		return distribution.ErrDigestMismatch.WithDetail(map[string]string{
			"expected": req.Digest,
			"actual":   resp.Digest,
		})
	}

	stat, err := p.objects.Stat(ctx, req.Digest)
	if err != nil {
		return wrapError(err, "stat verified blob object")
	}
	return p.upsertBlobRecords(ctx, req, stat, contentTypeFromHeader(resp.Headers))
}

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
	if _, err := p.metadata.UpsertBlob(ctx, *blob); err != nil {
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
