package cache

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
)

func (p tagProxy) List(ctx context.Context, req TagRequest) (*TagsResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}

	cacheKey := tagsCacheKey(req)
	if cached, ok, err := p.cached(ctx, cacheKey); err != nil || ok {
		if cached != nil {
			p.publishCacheAccess(ctx, req, cached.Cache)
		}
		return cached, err
	}

	result, err := p.fetch(ctx, req)
	if err != nil {
		return nil, err
	}
	p.setCache(ctx, req, cacheKey, result)
	p.publishCacheAccess(ctx, req, result.Cache)
	return result, nil
}

func (p tagProxy) cached(ctx context.Context, cacheKey string) (*TagsResult, bool, error) {
	if p.cache == nil || p.ttl <= 0 {
		return nil, false, nil
	}

	data, ok, err := p.cache.Get(ctx, cacheKey)
	if err != nil {
		return nil, false, wrapError(err, "get tags cache entry")
	}
	if !ok {
		return nil, false, nil
	}

	result, err := tagsFromEnvelope(data)
	if err != nil {
		if deleteErr := p.cache.Delete(ctx, cacheKey); deleteErr != nil {
			return nil, false, wrapError(deleteErr, "delete invalid tags cache entry")
		}
		return nil, false, nil
	}
	result.Cache = CacheHit
	return result, true, nil
}

func (p tagProxy) fetch(ctx context.Context, req TagRequest) (*TagsResult, error) {
	resp, err := p.client.ListTags(ctx, upstream.ListTagsRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		N:             req.N,
		Last:          req.Last,
	})
	if err != nil {
		return nil, wrapError(err, "fetch tags from upstream")
	}

	body, err := readHTTPBody(resp.Body, "tags body")
	if err != nil {
		return nil, err
	}
	return &TagsResult{
		Body:    body,
		Headers: rewriteTagsHeaders(resp.Headers, req),
		Cache:   CacheBypass,
	}, nil
}

func (p tagProxy) setCache(ctx context.Context, req TagRequest, cacheKey string, result *TagsResult) {
	if p.cache == nil || p.ttl <= 0 {
		return
	}
	data, err := tagsEnvelopeFromResult(result)
	if err != nil {
		return
	}
	if err := p.cache.Set(ctx, cacheKey, data, p.ttl); err != nil {
		return
	}
	p.publishCacheStore(ctx, req, result)
}
