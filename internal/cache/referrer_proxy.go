package cache

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (p referrerProxy) Get(ctx context.Context, req ReferrerRequest) (*ReferrersResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}

	cacheKey := referrersCacheKey(req)
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

func (p referrerProxy) cached(ctx context.Context, cacheKey string) (*ReferrersResult, bool, error) {
	if p.cache == nil || p.ttl <= 0 {
		return nil, false, nil
	}

	data, ok, err := p.cache.Get(ctx, cacheKey)
	if err != nil {
		return nil, false, fmt.Errorf("get referrers cache entry: %w", err)
	}
	if !ok {
		return nil, false, nil
	}

	result, err := referrersFromEnvelope(data)
	if err != nil {
		if deleteErr := p.cache.Delete(ctx, cacheKey); deleteErr != nil {
			return nil, false, fmt.Errorf("delete invalid referrers cache entry: %w", deleteErr)
		}
		return nil, false, nil
	}
	result.Cache = CacheHit
	return result, true, nil
}

func (p referrerProxy) fetch(ctx context.Context, req ReferrerRequest) (*ReferrersResult, error) {
	resp, err := p.client.GetReferrers(ctx, upstream.ReferrersRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
	})
	if err != nil {
		if p.fallbackTag && isManifestUnknown(err) {
			return p.fetchFallbackTag(ctx, req)
		}
		return nil, fmt.Errorf("fetch referrers from upstream: %w", err)
	}

	body, err := readHTTPBody(resp.Body, "referrers body")
	if err != nil {
		return nil, err
	}
	return &ReferrersResult{
		Body:      body,
		MediaType: resp.MediaType,
		Headers:   resp.Headers,
		Cache:     CacheBypass,
	}, nil
}

func (p referrerProxy) fetchFallbackTag(ctx context.Context, req ReferrerRequest) (*ReferrersResult, error) {
	fallbackReference, err := referrersFallbackReference(req.Digest)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.GetManifest(ctx, upstream.GetManifestRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Reference:     fallbackReference,
		Accept:        distribution.MediaTypeOCIIndex,
		Method:        http.MethodGet,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch referrers fallback tag from upstream: %w", err)
	}

	body, err := readHTTPBody(resp.Body, "referrers fallback body")
	if err != nil {
		return nil, err
	}
	return &ReferrersResult{
		Body:      body,
		MediaType: referrersMediaType(resp.MediaType),
		Headers:   resp.Headers,
		Cache:     CacheBypass,
	}, nil
}

func (p referrerProxy) setCache(ctx context.Context, cacheKey string, result *ReferrersResult) {
	if p.cache == nil || p.ttl <= 0 {
		return
	}
	data, err := referrersEnvelopeFromResult(result)
	if err != nil {
		return
	}
	if err := p.cache.Set(ctx, cacheKey, data, p.ttl); err != nil {
		return
	}
}

func isManifestUnknown(err error) bool {
	list := distribution.FromError(err)
	if list == nil {
		return false
	}
	for _, item := range list.Errors {
		if item.Code == distribution.CodeManifestUnknown {
			return true
		}
	}
	return false
}

func referrersFallbackReference(digest string) (string, error) {
	normalized, err := reference.NormalizeDigest(digest)
	if err != nil {
		return "", fmt.Errorf("normalize referrers fallback digest: %w", err)
	}
	algorithm, encoded, _ := strings.Cut(normalized, ":")
	return algorithm + "-" + encoded, nil
}

func referrersMediaType(mediaType string) string {
	if mediaType == "" {
		return distribution.MediaTypeOCIIndex
	}
	return mediaType
}
