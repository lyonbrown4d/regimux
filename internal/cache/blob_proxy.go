package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
		return cached, err
	}
	if p.shouldBypassStore(req) {
		return p.fetchPassthrough(ctx, req)
	}
	return p.fetchStored(ctx, req)
}

func (p blobProxy) shouldBypassStore(req BlobRequest) bool {
	return p.metadata == nil || p.objects == nil || req.Method == http.MethodHead
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
		return nil, fmt.Errorf("coalesce blob request: %w", err)
	}
	return p.openStored(ctx, req, CacheMiss)
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
		return nil, fmt.Errorf("fetch blob from upstream: %w", err)
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
		return nil, false, fmt.Errorf("lookup repository blob record: %w", err)
	}
	if !ok {
		return nil, false, nil
	}
	return p.openExistingStored(ctx, req, CacheHit)
}

func (p blobProxy) lookupSharedBlob(ctx context.Context, req BlobRequest) (*BlobReadResult, bool, error) {
	exists, err := p.objects.Exists(ctx, req.Digest)
	if err != nil {
		return nil, false, fmt.Errorf("check blob object: %w", err)
	}
	if !exists {
		return nil, false, nil
	}
	if err := p.verifyRepoBlob(ctx, req); err != nil {
		return nil, false, err
	}
	return p.openExistingStored(ctx, req, CacheHit)
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
		return fmt.Errorf("verify blob upstream membership: %w", err)
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
		return fmt.Errorf("stat verified blob object: %w", err)
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
		return fmt.Errorf("fetch blob for storage: %w", err)
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
		return fmt.Errorf("store blob object: %w", putErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return p.upsertBlobRecords(ctx, req, info, contentTypeFromHeader(resp.Headers))
}

func (p blobProxy) upsertBlobRecords(ctx context.Context, req BlobRequest, info *object.Info, mediaType string) error {
	if p.metadata == nil || info == nil {
		return nil
	}
	if _, err := p.metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       info.Digest,
		Size:         info.Size,
		MediaType:    mediaType,
		ObjectKey:    info.Digest,
		LastAccessAt: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("upsert blob record: %w", err)
	}
	_, err := p.metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:          req.UpstreamAlias,
		Repository:     req.Repo,
		Digest:         info.Digest,
		LastVerifiedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("upsert repository blob record: %w", err)
	}
	return nil
}

func (p blobProxy) openStored(ctx context.Context, req BlobRequest, cacheStatus CacheStatus) (*BlobReadResult, error) {
	info, err := p.objects.Stat(ctx, req.Digest)
	if err != nil {
		return nil, fmt.Errorf("stat stored blob object: %w", err)
	}
	headers := blobHeaders(info)

	status, size, opts, err := blobReadOptions(req, info.Size, headers)
	if err != nil {
		return nil, err
	}
	reader := io.NopCloser(bytes.NewReader(nil))
	if req.Method != http.MethodHead {
		reader, info, err = p.objects.Get(ctx, req.Digest, opts)
		if err != nil {
			return nil, fmt.Errorf("open stored blob object: %w", err)
		}
		size = info.Size
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

func blobHeaders(info *object.Info) http.Header {
	headers := http.Header{}
	headers.Set("Content-Length", strconv.FormatInt(info.Size, 10))
	headers.Set("ETag", info.ETag)
	return headers
}

func blobReadOptions(req BlobRequest, fullSize int64, headers http.Header) (int, int64, object.GetOptions, error) {
	status := http.StatusOK
	size := fullSize
	opts := object.GetOptions{}
	if req.Range == nil {
		return status, size, opts, nil
	}

	resolved, err := req.Range.Resolve(fullSize)
	if err != nil {
		return 0, 0, object.GetOptions{}, distribution.ErrRangeInvalid.WithDetail(err.Error())
	}
	status = http.StatusPartialContent
	size = resolved.Length()
	headers.Set("Content-Length", strconv.FormatInt(size, 10))
	headers.Set("Content-Range", resolved.ContentRange(fullSize))
	opts.Range = req.Range
	return status, size, opts, nil
}
