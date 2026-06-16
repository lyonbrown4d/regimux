package cache

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
)

func (p blobProxy) Get(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	if cached, ok, err := p.lookupSmallBlobCache(ctx, req); err != nil || ok {
		return p.withCacheAccess(ctx, req, cached, err)
	}
	if cached, ok, err := p.lookup(ctx, req); err != nil || ok {
		return p.withCacheAccess(ctx, req, cached, err)
	}

	if p.shouldStreamAndCache(req) {
		result, err := p.fetchStreamAndStore(ctx, req)
		return p.withCacheAccess(ctx, req, result, err)
	}

	if p.shouldBypassStore(req) {
		result, err := p.fetchPassthrough(ctx, req)
		return p.withCacheAccess(ctx, req, result, err)
	}
	result, err := p.fetchStored(ctx, req)
	return p.withCacheAccess(ctx, req, result, err)
}

func (p blobProxy) withCacheAccess(ctx context.Context, req BlobRequest, result *BlobReadResult, err error) (*BlobReadResult, error) {
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
		p.metadata != nil &&
		p.objects != nil
}

func (p blobProxy) fetchStored(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	key := blobFillCacheKey(req.Digest)
	if p.group == nil {
		return p.fetchAndOpenStored(ctx, req)
	}

	filled, err, _ := p.group.Do(key, func() (bool, error) {
		return p.ensureStored(ctx, req)
	})
	if err != nil {
		return nil, wrapError(err, "coalesce blob request")
	}
	status := CacheHit
	if filled {
		status = CacheMiss
	}
	return p.openStored(ctx, req, status)
}

func (p blobProxy) fetchStreamAndStore(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	if req.Range != nil {
		return p.fetchRangeStream(ctx, req)
	}
	return p.fetchFullStreamAndStore(ctx, req)
}

func (p blobProxy) fetchRangeStream(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	p.logBlobCacheHit(ctx, req, "stream_and_cache_range_fill")
	return p.fetchStored(ctx, req)
}

func (p blobProxy) fetchFullStreamAndStore(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	key := blobFillCacheKey(req.Digest)
	for {
		fill, owner := p.fills.begin(key)
		if owner {
			result, retry, err := p.fetchFullStreamAndStoreLocalOwner(ctx, req, key, fill)
			if err != nil || !retry {
				return result, err
			}
			continue
		}
		result, retry, err := p.waitForStreamedFullBlob(ctx, req, fill)
		if err != nil || !retry {
			return result, err
		}
	}
}

func (p blobProxy) waitForStreamedFullBlob(ctx context.Context, req BlobRequest, fill *blobFill) (*BlobReadResult, bool, error) {
	if err := fill.wait(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, wrapError(ctxErr, "wait for streamed blob cache fill")
		}
		p.logBlobStreamCacheError(ctx, req, "streamed blob cache fill failed; retrying blob fetch", err)
		return nil, true, nil
	}
	if cached, ok, err := p.lookupSmallBlobCache(ctx, req); err != nil || ok {
		return cached, false, err
	}
	if cached, ok, err := p.lookup(ctx, req); err != nil || ok {
		return cached, false, err
	}
	p.logBlobLookupSkip(ctx, req, "streamed_blob_fill_completed_without_object")
	return nil, true, nil
}

func (p blobProxy) fetchFullStreamAndStoreLocalOwner(ctx context.Context, req BlobRequest, key string, fill *blobFill) (*BlobReadResult, bool, error) {
	for {
		lease, owner := p.acquireBlobFillLease(ctx, req)
		if !owner {
			if err := waitForBlobFillLeasePoll(ctx); err != nil {
				p.fills.finish(key, fill, err)
				return nil, false, err
			}
			continue
		}

		if cached, ok, err := p.lookupSmallBlobCache(ctx, req); err != nil || ok {
			p.releaseBlobFillLease(req, lease)
			p.fills.finish(key, fill, err)
			return cached, false, err
		}
		if cached, ok, err := p.lookup(ctx, req); err != nil || ok {
			p.releaseBlobFillLease(req, lease)
			p.fills.finish(key, fill, err)
			return cached, false, err
		}
		return p.fetchFullStreamAndStoreOwner(ctx, req, key, fill, lease)
	}
}

func (p blobProxy) fetchFullStreamAndStoreOwner(ctx context.Context, req BlobRequest, key string, fill *blobFill, lease backend.Lease) (*BlobReadResult, bool, error) {
	releaseLease := p.startBlobFillLease(ctx, req, lease)
	resp, err := p.client.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Method:        req.Method,
	})
	if err != nil {
		releaseLease()
		p.fills.finish(key, fill, err)
		return nil, false, wrapError(err, "stream blob from upstream")
	}
	if err := validateStoredBlobDigest(req.Digest, resp.Digest); err != nil {
		releaseLease()
		p.fills.finish(key, fill, err)
		if closeErr := closeHTTPBody(resp.Body, "blob stream response body"); closeErr != nil {
			return nil, false, joinError("close blob stream after digest mismatch", err, closeErr)
		}
		return nil, false, err
	}

	mediaType := contentTypeFromHeader(resp.Headers)
	reader := p.streamBlobToCache(ctx, req, resp, mediaType, func(err error) {
		releaseLease()
		p.fills.finish(key, fill, err)
	})
	p.logBlobCacheHit(ctx, req, "stream_and_cache_full")
	return &BlobReadResult{
		Reader:  reader,
		Digest:  resp.Digest,
		Size:    resp.Size,
		Status:  resp.StatusCode,
		Headers: resp.Headers,
		Cache:   CacheMiss,
	}, false, nil
}

func blobFillCacheKey(digest string) string {
	return "blob:" + digest
}

func (p blobProxy) fetchAndOpenStored(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	filled, err := p.ensureStored(ctx, req)
	if err != nil {
		return nil, err
	}
	status := CacheHit
	if filled {
		status = CacheMiss
	}
	return p.openStored(ctx, req, status)
}

func (p blobProxy) ensureStored(ctx context.Context, req BlobRequest) (bool, error) {
	if err := p.waitForOngoingStreamedFill(ctx, req); err != nil {
		return false, err
	}
	return p.ensureStoredWithLease(ctx, req)
}

func (p blobProxy) waitForOngoingStreamedFill(ctx context.Context, req BlobRequest) error {
	fill, ok := p.fills.current(blobFillCacheKey(req.Digest))
	if !ok {
		return nil
	}
	if err := fill.wait(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapError(ctxErr, "wait for streamed blob cache fill")
		}
		p.logBlobStreamCacheError(ctx, req, "streamed blob cache fill failed; falling back to direct blob storage", err)
	}
	return nil
}

func (p blobProxy) ensureStoredWithLease(ctx context.Context, req BlobRequest) (bool, error) {
	for {
		lease, owner := p.acquireBlobFillLease(ctx, req)
		if !owner {
			if err := waitForBlobFillLeasePoll(ctx); err != nil {
				return false, err
			}
			continue
		}

		if _, ok, err := p.lookup(ctx, BlobRequest{
			UpstreamAlias: req.UpstreamAlias,
			Repo:          req.Repo,
			Digest:        req.Digest,
			Method:        http.MethodHead,
		}); err != nil || ok {
			p.releaseBlobFillLease(req, lease)
			return false, err
		}
		releaseLease := p.startBlobFillLease(ctx, req, lease)
		err := p.fetchAndStore(ctx, req)
		releaseLease()
		return true, err
	}
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
