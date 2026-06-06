package cache

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (p manifestProxy) lookup(ctx context.Context, req ManifestRequest, cacheKey string) (*CachedManifest, bool, error) {
	if cached, ok, err := p.lookupEnvelope(ctx, req, cacheKey); err != nil || ok {
		return cached, ok, err
	}
	return p.lookupStored(ctx, req, cacheKey)
}

func (p manifestProxy) lookupEnvelope(ctx context.Context, req ManifestRequest, cacheKey string) (*CachedManifest, bool, error) {
	if p.cache == nil {
		return nil, false, nil
	}

	data, ok, err := p.cache.Get(ctx, cacheKey)
	if err != nil {
		return nil, false, wrapError(err, "get manifest cache entry")
	}
	if !ok {
		return nil, false, nil
	}

	manifest, err := manifestFromEnvelope(data)
	if err != nil {
		if deleteErr := p.cache.Delete(ctx, cacheKey); deleteErr != nil {
			return nil, false, wrapError(deleteErr, "delete invalid manifest cache entry")
		}
		return nil, false, nil
	}
	return manifestForMethod(manifest, req.Method), true, nil
}

func (p manifestProxy) lookupStored(ctx context.Context, req ManifestRequest, cacheKey string) (*CachedManifest, bool, error) {
	if p.metadata == nil || p.objects == nil {
		return nil, false, nil
	}

	record, ok, err := p.lookupMetadata(ctx, req)
	if err != nil || !ok {
		return nil, false, err
	}
	manifest, ok, err := p.manifestFromRecord(ctx, req, record, CacheHit)
	if err != nil || !ok {
		return nil, false, err
	}
	p.setManifestCache(ctx, cacheKey, *record, manifest.Body, ttlUntil(record.ExpiresAt, p.ttl))
	return manifest, true, nil
}

func manifestForMethod(manifest *CachedManifest, method string) *CachedManifest {
	if method == http.MethodHead {
		manifest.Body = nil
	}
	manifest.Cache = CacheHit
	return manifest
}

func (p manifestProxy) lookupStale(ctx context.Context, req ManifestRequest) (*CachedManifest, bool, error) {
	if !p.canServeStale() {
		return nil, false, nil
	}

	record, ok, err := p.lookupStaleRecord(ctx, req, time.Now())
	if err != nil || !ok {
		return nil, false, err
	}
	manifest, ok, err := p.manifestFromRecord(ctx, req, record, CacheStale)
	if err != nil || !ok {
		return nil, false, err
	}
	manifest.Headers.Set(distribution.HeaderWarning, distribution.WarningResponseIsStale)
	return manifest, true, nil
}

func (p manifestProxy) canServeStale() bool {
	return p.staleIfError && p.maxStale > 0 && p.metadata != nil && p.objects != nil
}

func (p manifestProxy) lookupStaleRecord(ctx context.Context, req ManifestRequest, now time.Time) (*meta.ManifestRecord, bool, error) {
	if reference.IsDigest(req.Reference) {
		return p.lookupStaleDigestRecord(ctx, req, now)
	}
	return p.lookupStaleTagRecord(ctx, req, now)
}

func (p manifestProxy) lookupStaleDigestRecord(ctx context.Context, req ManifestRequest, now time.Time) (*meta.ManifestRecord, bool, error) {
	digest, err := digestFromReference(req.Reference, nil)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := p.metadata.Manifest(ctx, meta.ManifestKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     digest,
	})
	if err != nil {
		return nil, false, wrapError(err, "lookup stale manifest by digest")
	}
	if !ok || !p.validStaleManifest(record, now) {
		return nil, false, nil
	}
	return record, true, nil
}

func (p manifestProxy) lookupStaleTagRecord(ctx context.Context, req ManifestRequest, now time.Time) (*meta.ManifestRecord, bool, error) {
	tag, ok, err := p.metadata.Tag(ctx, meta.TagKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
	})
	if err != nil {
		return nil, false, wrapError(err, "lookup stale manifest tag")
	}
	if !ok || !p.withinStaleWindow(tag.ExpiresAt, now) {
		return nil, false, nil
	}

	record, ok, err := p.metadata.Manifest(ctx, meta.ManifestKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     tag.Digest,
	})
	if err != nil {
		return nil, false, wrapError(err, "lookup stale tagged manifest")
	}
	if !ok || !p.validStaleManifest(record, now) {
		return nil, false, nil
	}
	return record, true, nil
}

func (p manifestProxy) validStaleManifest(record *meta.ManifestRecord, now time.Time) bool {
	if !p.withinStaleWindow(record.ExpiresAt, now) {
		return false
	}
	return true
}

func (p manifestProxy) withinStaleWindow(expiresAt, now time.Time) bool {
	if expiresAt.IsZero() || now.Before(expiresAt) {
		return false
	}
	return now.Before(expiresAt.Add(p.maxStale))
}

func (p manifestProxy) manifestFromRecord(ctx context.Context, req ManifestRequest, record *meta.ManifestRecord, status CacheStatus) (*CachedManifest, bool, error) {
	if record == nil {
		return nil, false, nil
	}

	body, ok, err := p.storedManifestBody(ctx, req, record)
	if err != nil || !ok {
		return nil, ok, err
	}
	return &CachedManifest{
		Digest:    record.Digest,
		MediaType: record.MediaType,
		Size:      record.Size,
		Body:      body,
		Headers:   http.Header(record.Headers).Clone(),
		Cache:     status,
	}, true, nil
}

func (p manifestProxy) storedManifestBody(ctx context.Context, req ManifestRequest, record *meta.ManifestRecord) ([]byte, bool, error) {
	if req.Method == http.MethodHead {
		return nil, true, nil
	}

	reader, _, err := p.objects.Get(ctx, record.Digest, object.GetOptions{})
	if errors.Is(err, object.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "open stored manifest object")
	}
	body, err := readHTTPBody(reader, "stored manifest object")
	if err != nil {
		return nil, false, err
	}
	return body, true, nil
}

func (p manifestProxy) lookupMetadata(ctx context.Context, req ManifestRequest) (*meta.ManifestRecord, bool, error) {
	now := time.Now()
	if reference.IsDigest(req.Reference) {
		digest, err := digestFromReference(req.Reference, nil)
		if err != nil {
			return nil, false, err
		}
		return p.lookupManifestRecord(ctx, meta.ManifestKey{
			Alias:      req.UpstreamAlias,
			Repository: req.Repo,
			Digest:     digest,
		}, now)
	}

	tag, ok, err := p.metadata.Tag(ctx, meta.TagKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
	})
	if err != nil {
		return nil, false, wrapError(err, "lookup manifest tag")
	}
	if !ok || tagExpired(tag, now) {
		return nil, false, nil
	}
	return p.lookupManifestRecord(ctx, meta.ManifestKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     tag.Digest,
	}, now)
}

func tagExpired(tag *meta.TagRecord, now time.Time) bool {
	return !tag.ExpiresAt.IsZero() && !now.Before(tag.ExpiresAt)
}

func (p manifestProxy) lookupManifestRecord(ctx context.Context, key meta.ManifestKey, now time.Time) (*meta.ManifestRecord, bool, error) {
	record, ok, err := p.metadata.Manifest(ctx, key)
	if err != nil {
		return nil, false, wrapError(err, "lookup manifest record")
	}
	if !ok || record.Expired(now) {
		return nil, false, nil
	}
	return record, true, nil
}
