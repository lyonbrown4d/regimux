package cache

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func (p manifestProxy) store(ctx context.Context, req ManifestRequest, cacheKey string, manifest *CachedManifest) {
	if manifest == nil {
		return
	}

	ttl := p.effectiveTTL()
	record := newManifestRecord(req, cacheKey, manifest, ttl)
	objectStored := req.Method == http.MethodHead
	if p.storeManifestObject(ctx, manifest, &record) {
		objectStored = true
	}
	if objectStored {
		p.storeManifestMetadata(ctx, req, record)
	}
	p.setManifestCache(ctx, cacheKey, record, manifest.Body, ttl)
}

func newManifestRecord(req ManifestRequest, cacheKey string, manifest *CachedManifest, ttl time.Duration) meta.ManifestRecord {
	return meta.ManifestRecord{
		Key:        cacheKey,
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
		AcceptKey:  reference.AcceptKey(req.Accept),
		Digest:     manifest.Digest,
		MediaType:  manifest.MediaType,
		Size:       manifest.Size,
		ObjectKey:  manifest.Digest,
		Headers:    map[string][]string(manifest.Headers.Clone()),
		ExpiresAt:  time.Now().UTC().Add(ttl),
	}
}

func (p manifestProxy) storeManifestObject(ctx context.Context, manifest *CachedManifest, record *meta.ManifestRecord) bool {
	if p.objects == nil || manifest.Digest == "" || len(manifest.Body) == 0 {
		return false
	}
	info, err := p.objects.Put(ctx, manifest.Digest, bytes.NewReader(manifest.Body), object.PutOptions{ContentType: manifest.MediaType})
	if err != nil {
		return false
	}
	record.ObjectKey = info.Digest
	if record.Size < 0 {
		record.Size = info.Size
	}
	return true
}

func (p manifestProxy) storeManifestMetadata(ctx context.Context, req ManifestRequest, record meta.ManifestRecord) {
	if p.metadata == nil || record.Digest == "" {
		return
	}
	if _, err := p.metadata.UpsertManifest(ctx, record); err != nil || reference.IsDigest(req.Reference) {
		return
	}
	_, err := p.metadata.UpsertTag(ctx, meta.TagRecord{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
		Digest:     record.Digest,
		ExpiresAt:  record.ExpiresAt,
	})
	if err != nil {
		return
	}
}

func (p manifestProxy) setManifestCache(ctx context.Context, cacheKey string, record meta.ManifestRecord, body []byte, ttl time.Duration) {
	if p.cache == nil || len(body) == 0 {
		return
	}
	data, err := manifestEnvelopeFromRecord(record, body)
	if err != nil {
		return
	}
	if err := p.cache.Set(ctx, cacheKey, data, ttl); err != nil {
		return
	}
}

func (p manifestProxy) effectiveTTL() time.Duration {
	if p.ttl > 0 {
		return p.ttl
	}
	return defaultManifestTTL()
}
