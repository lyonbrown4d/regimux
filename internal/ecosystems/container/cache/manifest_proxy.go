package cache

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (p manifestProxy) Get(ctx context.Context, req ManifestRequest) (*CachedManifest, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}

	cacheKey := manifestCacheKey(req)
	if p.group == nil {
		return p.get(ctx, req, cacheKey, manifestRequestModeClient)
	}

	result, err, _ := p.group.Do(cacheKey, func() (*CachedManifest, error) {
		return p.get(ctx, req, cacheKey, manifestRequestModeClient)
	})
	if err != nil {
		return nil, wrapError(err, "coalesce manifest request")
	}
	return result, nil
}

func (p manifestProxy) Refresh(ctx context.Context, req ManifestRequest) (*CachedManifest, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	req.SkipPullRecord = true

	cacheKey := manifestCacheKey(req)
	if p.group == nil {
		return p.get(ctx, req, cacheKey, manifestRequestModeRefresh)
	}

	result, err, _ := p.group.Do(cacheKey+"\x00refresh", func() (*CachedManifest, error) {
		return p.get(ctx, req, cacheKey, manifestRequestModeRefresh)
	})
	if err != nil {
		return nil, wrapError(err, "coalesce manifest refresh")
	}
	return result, nil
}

type manifestRequestMode int

const (
	manifestRequestModeClient manifestRequestMode = iota
	manifestRequestModeRefresh
)

func (p manifestProxy) get(ctx context.Context, req ManifestRequest, cacheKey string, mode manifestRequestMode) (*CachedManifest, error) {
	if cached, ok, err := p.lookupClientCache(ctx, req, cacheKey, mode); err != nil || ok {
		return cached, err
	}

	if result, ok, err := p.revalidate(ctx, req, cacheKey, mode); err != nil {
		return nil, err
	} else if ok {
		return p.finishManifestResult(ctx, req, result), nil
	}

	result, err := p.fetch(ctx, req)
	if err != nil {
		if mode == manifestRequestModeRefresh {
			return nil, err
		}
		return p.lookupStaleOrError(ctx, req, err)
	}
	p.recordManifestUpstreamPull(ctx, req)
	p.store(ctx, req, cacheKey, result)
	return p.finishManifestResult(ctx, req, result), nil
}

func (p manifestProxy) lookupClientCache(ctx context.Context, req ManifestRequest, cacheKey string, mode manifestRequestMode) (*CachedManifest, bool, error) {
	if mode == manifestRequestModeRefresh {
		return nil, false, nil
	}
	cached, ok, err := p.lookup(ctx, req, cacheKey)
	if err != nil {
		return nil, false, err
	}
	if ok {
		return p.finishManifestResult(ctx, req, cached), true, nil
	}
	stale, ok, err := p.lookupAnyStored(ctx, req)
	if err != nil || !ok {
		return nil, false, err
	}
	return p.finishManifestResult(ctx, req, stale), true, nil
}

func (p manifestProxy) finishManifestResult(ctx context.Context, req ManifestRequest, result *CachedManifest) *CachedManifest {
	p.recordManifestPull(ctx, req, result)
	p.publishCacheAccess(ctx, req, result)
	return result
}

func (p manifestProxy) lookupStaleOrError(ctx context.Context, req ManifestRequest, cause error) (*CachedManifest, error) {
	stale, ok, err := p.lookupStale(ctx, req)
	if err != nil {
		return nil, err
	}
	if ok {
		return p.finishManifestResult(ctx, req, stale), nil
	}
	return nil, cause
}

func (p manifestProxy) recordManifestPull(ctx context.Context, req ManifestRequest, result *CachedManifest) {
	if key, ok := manifestPullKey(req); ok {
		p.publishDependencyPulled(ctx, req, result)
		if p.metadata != nil {
			if _, err := p.metadata.RecordPull(ctx, key, time.Now().UTC()); err != nil {
				return
			}
		}
		p.publishArtifactPulled(ctx, req, result)
	}
}

func (p manifestProxy) recordManifestUpstreamPull(ctx context.Context, req ManifestRequest) {
	if key, ok := manifestPullKey(req); ok && p.metadata != nil {
		if _, err := p.metadata.RecordUpstreamPull(ctx, key, time.Now().UTC()); err != nil {
			return
		}
	}
}

func manifestPullKey(req ManifestRequest) (meta.PullKey, bool) {
	if req.SkipPullRecord || reference.IsDigest(req.Reference) || req.Reference == "" {
		return meta.PullKey{}, false
	}
	return meta.PullKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
	}, true
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
	if err := validateManifestMediaType(resp.MediaType); err != nil {
		return nil, closeHTTPBodyWithError(resp.Body, err, "unsupported manifest response body")
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
