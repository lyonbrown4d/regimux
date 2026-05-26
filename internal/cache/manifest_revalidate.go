package cache

import (
	"context"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func (p manifestProxy) revalidate(ctx context.Context, req ManifestRequest, cacheKey string) (*CachedManifest, bool, error) {
	tag, record, ok, err := p.revalidationCandidate(ctx, req)
	if err != nil || !ok {
		return nil, false, err
	}

	resp, ok, err := p.revalidationHead(ctx, req)
	if err != nil || !ok {
		return nil, false, err
	}
	if !sameRevalidatedDigest(req, record.Digest, resp.Digest) {
		return nil, false, nil
	}
	p.recordManifestUpstreamPull(ctx, req)

	p.refreshRevalidatedRecord(record, resp)
	if saveErr := p.saveRevalidatedRecords(ctx, tag, record); saveErr != nil {
		return nil, false, saveErr
	}

	result, ok, err := p.manifestFromRecord(ctx, req, record, CacheHit)
	if err != nil || !ok {
		return nil, false, err
	}
	p.setManifestCache(ctx, cacheKey, *record, result.Body, p.effectiveTTL())
	return result, true, nil
}

func (p manifestProxy) revalidationCandidate(ctx context.Context, req ManifestRequest) (*meta.TagRecord, *meta.ManifestRecord, bool, error) {
	if reference.IsDigest(req.Reference) || p.metadata == nil || p.objects == nil {
		return nil, nil, false, nil
	}

	tag, ok, err := p.expiredTagForRevalidation(ctx, req)
	if err != nil || !ok {
		return nil, nil, false, err
	}
	record, ok, err := p.manifestRecordForRevalidation(ctx, req, tag)
	if err != nil || !ok {
		return nil, nil, false, err
	}

	exists, err := p.objects.Exists(ctx, record.Digest)
	if err != nil {
		return nil, nil, false, wrapError(err, "check manifest object for revalidation")
	}
	return tag, record, exists, nil
}

func (p manifestProxy) expiredTagForRevalidation(ctx context.Context, req ManifestRequest) (*meta.TagRecord, bool, error) {
	tag, ok, err := p.metadata.Tag(ctx, meta.TagKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
	})
	if err != nil {
		return nil, false, wrapError(err, "lookup manifest tag for revalidation")
	}
	return tag, ok && tagExpired(tag, time.Now()), nil
}

func (p manifestProxy) manifestRecordForRevalidation(ctx context.Context, req ManifestRequest, tag *meta.TagRecord) (*meta.ManifestRecord, bool, error) {
	record, ok, err := p.metadata.Manifest(ctx, meta.ManifestKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     tag.Digest,
	})
	if err != nil {
		return nil, false, wrapError(err, "lookup manifest for revalidation")
	}
	if !ok || !acceptMatches(record.AcceptKey, reference.AcceptKey(req.Accept)) {
		return nil, false, nil
	}
	return record, true, nil
}

func (p manifestProxy) revalidationHead(ctx context.Context, req ManifestRequest) (*upstream.ManifestResponse, bool, error) {
	resp, fetchErr := p.client.GetManifest(ctx, upstream.GetManifestRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Reference:     req.Reference,
		Accept:        req.Accept,
		Method:        http.MethodHead,
	})
	if fetchErr == nil {
		if err := closeHTTPBody(resp.Body, "manifest revalidation body"); err != nil {
			return nil, false, err
		}
		return resp, true, nil
	}
	return nil, false, nil
}

func sameRevalidatedDigest(req ManifestRequest, expected, upstreamDigest string) bool {
	digest, err := manifestDigest(ManifestRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Reference:     req.Reference,
		Accept:        req.Accept,
		Method:        http.MethodHead,
	}, upstreamDigest, nil)
	return err == nil && digest == expected
}

func (p manifestProxy) refreshRevalidatedRecord(record *meta.ManifestRecord, resp *upstream.ManifestResponse) {
	now := time.Now().UTC()
	record.ExpiresAt = now.Add(p.effectiveTTL())
	record.UpdatedAt = now
	if resp.MediaType != "" {
		record.MediaType = resp.MediaType
	}
	if resp.Size >= 0 {
		record.Size = resp.Size
	}
	if resp.Headers != nil {
		record.Headers = map[string][]string(resp.Headers.Clone())
	}
}

func (p manifestProxy) saveRevalidatedRecords(ctx context.Context, tag *meta.TagRecord, record *meta.ManifestRecord) error {
	if _, err := p.metadata.UpsertManifest(ctx, *record); err != nil {
		return wrapError(err, "upsert revalidated manifest")
	}
	tag.ExpiresAt = record.ExpiresAt
	if _, err := p.metadata.UpsertTag(ctx, *tag); err != nil {
		return wrapError(err, "upsert revalidated manifest tag")
	}
	return nil
}
