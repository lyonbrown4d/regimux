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
	flightKey := manifestFlightKey(cacheKey, req.Method)
	if req.deferMetadata {
		flightKey += "\x00prepare"
	}
	if p.group == nil {
		return p.get(ctx, req, cacheKey, manifestRequestModeClient)
	}

	return p.coalesce(ctx, flightKey, "coalesce manifest request", func(sharedCtx context.Context) (*CachedManifest, error) {
		return p.get(sharedCtx, req, cacheKey, manifestRequestModeClient)
	})
}

func (p manifestProxy) Refresh(ctx context.Context, req ManifestRequest) (*CachedManifest, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	req.SkipPullRecord = true

	cacheKey := manifestCacheKey(req)
	flightKey := manifestFlightKey(cacheKey, req.Method) + "\x00refresh"
	if p.group == nil {
		return p.get(ctx, req, cacheKey, manifestRequestModeRefresh)
	}

	return p.coalesce(ctx, flightKey, "coalesce manifest refresh", func(sharedCtx context.Context) (*CachedManifest, error) {
		return p.get(sharedCtx, req, cacheKey, manifestRequestModeRefresh)
	})
}

func (p manifestProxy) coalesce(ctx context.Context, key, operation string, fill func(context.Context) (*CachedManifest, error)) (*CachedManifest, error) {
	result, err, _ := p.group.DoContext(ctx, key, fill)
	if err != nil {
		return nil, wrapError(err, "%s", operation)
	}
	return result, nil
}

func manifestFlightKey(cacheKey, method string) string {
	if method == "" {
		method = http.MethodGet
	}
	return cacheKey + "\x00" + method
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
	p.storeFetchedManifest(ctx, req, cacheKey, result)
	return p.finishManifestResult(ctx, req, result), nil
}

func (p manifestProxy) storeFetchedManifest(ctx context.Context, req ManifestRequest, cacheKey string, result *CachedManifest) {
	if req.deferMetadata {
		result.preparedMetadata = p.storePrepared(ctx, req, cacheKey, result)
		if result.preparedMetadata == nil {
			result.preparedMetadata = p.preparedUpstreamMetadata(req, nil, nil, false, true)
		}
		return
	}
	p.recordManifestUpstreamPull(ctx, req)
	p.store(ctx, req, cacheKey, result)
}

func (p manifestProxy) preparedUpstreamMetadata(req ManifestRequest, record *meta.ManifestRecord, tag *meta.TagRecord, propagateSaveError, upstreamPull bool) *preparedManifestMetadata {
	key, ok := manifestPullKey(req)
	return newPreparedManifestMetadata(p.metadata, p.logger, key, ok && upstreamPull, record, tag, propagateSaveError)
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
	if req.deferMetadata {
		return result
	}
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
	mediaErr := validateManifestMediaType(resp.MediaType)
	if mediaErr != nil {
		return nil, closeHTTPBodyWithError(resp.Body, mediaErr, "unsupported manifest response body")
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
