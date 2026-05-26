package cache

import (
	"context"
	"fmt"

	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func (p tagProxy) List(ctx context.Context, req TagRequest) (*TagsResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}

	cacheKey := tagsCacheKey(req)
	if cached, ok, err := p.cached(ctx, cacheKey); err != nil || ok {
		return cached, err
	}

	result, err := p.fetch(ctx, req)
	if err != nil {
		return nil, err
	}
	p.setCache(ctx, cacheKey, result)
	return result, nil
}

func (p tagProxy) cached(ctx context.Context, cacheKey string) (*TagsResult, bool, error) {
	if p.cache == nil || p.ttl <= 0 {
		return nil, false, nil
	}

	data, ok, err := p.cache.Get(ctx, cacheKey)
	if err != nil {
		return nil, false, fmt.Errorf("get tags cache entry: %w", err)
	}
	if !ok {
		return nil, false, nil
	}

	result, err := tagsFromEnvelope(data)
	if err != nil {
		if deleteErr := p.cache.Delete(ctx, cacheKey); deleteErr != nil {
			return nil, false, fmt.Errorf("delete invalid tags cache entry: %w", deleteErr)
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
		return nil, fmt.Errorf("fetch tags from upstream: %w", err)
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

func (p tagProxy) setCache(ctx context.Context, cacheKey string, result *TagsResult) {
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
}
