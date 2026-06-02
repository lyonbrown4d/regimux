package cache

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/upstream"
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
	if req.Range != nil {
		return p.fetchRangeStream(ctx, req)
	}
	return p.fetchFullStreamAndStore(ctx, req)
}

func (p blobProxy) fetchRangeStream(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
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
			return nil, joinError("close blob stream after metadata touch failure", err, closeErr)
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

func (p blobProxy) fetchFullStreamAndStore(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	resp, err := p.client.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Method:        req.Method,
	})
	if err != nil {
		return nil, wrapError(err, "stream blob from upstream")
	}
	if err := validateStoredBlobDigest(req.Digest, resp.Digest); err != nil {
		if closeErr := closeHTTPBody(resp.Body, "blob stream response body"); closeErr != nil {
			return nil, joinError("close blob stream after digest mismatch", err, closeErr)
		}
		return nil, err
	}

	mediaType := contentTypeFromHeader(resp.Headers)
	reader := p.streamBlobToCache(ctx, req, resp, mediaType)
	p.logBlobCacheHit(ctx, req, "stream_and_cache_full")
	return &BlobReadResult{
		Reader:  reader,
		Digest:  resp.Digest,
		Size:    resp.Size,
		Status:  resp.StatusCode,
		Headers: resp.Headers,
		Cache:   CacheMiss,
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
