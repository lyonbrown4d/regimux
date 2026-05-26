package cache

import (
	"context"
	"io"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func (p manifestProxy) Get(ctx context.Context, req ManifestRequest) (*CachedManifest, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}

	cacheKey := manifestCacheKey(req)
	if p.group == nil {
		return p.get(ctx, req, cacheKey)
	}

	value, err, _ := p.group.Do(cacheKey, func() (any, error) {
		return p.get(ctx, req, cacheKey)
	})
	if err != nil {
		return nil, wrapError(err, "coalesce manifest request")
	}

	result, ok := value.(*CachedManifest)
	if !ok {
		return nil, errorf("unexpected manifest cache result type %T", value)
	}
	return result, nil
}

func (p manifestProxy) get(ctx context.Context, req ManifestRequest, cacheKey string) (*CachedManifest, error) {
	if cached, ok, err := p.lookup(ctx, req, cacheKey); err != nil {
		return nil, err
	} else if ok {
		return cached, nil
	}

	if result, ok, err := p.revalidate(ctx, req, cacheKey); err != nil {
		return nil, err
	} else if ok {
		return result, nil
	}

	result, err := p.fetch(ctx, req)
	if err != nil {
		return p.lookupStaleOrError(ctx, req, err)
	}
	p.store(ctx, req, cacheKey, result)
	return result, nil
}

func (p manifestProxy) lookupStaleOrError(ctx context.Context, req ManifestRequest, cause error) (*CachedManifest, error) {
	stale, ok, err := p.lookupStale(ctx, req)
	if err != nil {
		return nil, err
	}
	if ok {
		return stale, nil
	}
	return nil, cause
}

func (p manifestProxy) fetch(ctx context.Context, req ManifestRequest) (*CachedManifest, error) {
	resp, err := p.client.GetManifest(ctx, upstream.GetManifestRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Reference:     req.Reference,
		Accept:        req.Accept,
		Method:        req.Method,
	})
	if err != nil {
		return nil, wrapError(err, "fetch manifest from upstream")
	}

	body, err := readManifestBody(resp.Body, req.Method)
	if err != nil {
		return nil, err
	}
	digest, err := manifestDigest(req, resp.Digest, body)
	if err != nil {
		return nil, err
	}

	size := resp.Size
	if size < 0 && body != nil {
		size = int64(len(body))
	}
	return &CachedManifest{
		Digest:    digest,
		MediaType: resp.MediaType,
		Size:      size,
		Body:      body,
		Headers:   resp.Headers,
		Cache:     CacheBypass,
	}, nil
}

func readManifestBody(body io.ReadCloser, method string) ([]byte, error) {
	if method == http.MethodHead {
		return nil, closeHTTPBody(body, "manifest response body")
	}
	return readHTTPBody(body, "manifest body")
}
