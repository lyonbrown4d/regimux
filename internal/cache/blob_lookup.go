package cache

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

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
	_, err = p.metadata.UpsertBlob(ctx, *blob)
	if err != nil {
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
