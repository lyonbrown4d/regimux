//revive:disable:file-length-limit npm service keeps route, rewrite, cache, and upstream orchestration in one implementation file.
package npm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
	"github.com/samber/lo"
	"github.com/samber/oops"
)

func NewService(deps ServiceDependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	factory := deps.Factory
	if factory == nil {
		factory = clientfactory.New(logger)
	}
	cache := deps.Cache
	if cache == nil {
		cache = artifactcache.New(artifactcache.Dependencies{
			Metadata: deps.Metadata,
			Objects:  deps.Objects,
			Logger:   logger,
			Now:      now,
		})
	}
	fills := cache.FillTracker()
	if fills == nil {
		fills = artifactcache.NewFillTracker()
	}
	return &Service{
		cfg:         deps.Config,
		metadata:    deps.Metadata,
		cache:       cache,
		client:      deps.Client,
		factory:     factory,
		logger:      logger.With("component", "npm"),
		publicURL:   strings.TrimRight(deps.Config.Server.PublicURL, "/"),
		metadataTTL: deps.MetadataTTL,
		fills:       fills,
		now:         now,
		events:      deps.Events,
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("npm").Errorf("service is nil")
	}
	requestRoute, err := parseRoute(req)
	if err != nil {
		return nil, err
	}
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("npm").
			With("alias", requestRoute.Alias).
			Errorf("npm upstream is not configured")
	}
	requestRoute.MetadataTTL = s.metadataTTL
	if requestRoute.MetadataTTL <= 0 {
		requestRoute.MetadataTTL = upstreamCfg.TagTTL
	}
	resp, err := s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeClient)
	s.recordPull(ctx, req, requestRoute, resp, err)
	return resp, err
}

func (s *Service) Upstreams() *collectionlist.List[Upstream] {
	if s == nil {
		return collectionlist.NewList[Upstream]()
	}
	ordered := s.cfg.OrderedNPMUpstreams()
	return collectionlist.NewList(lo.Map(ordered.Keys(), func(alias string, _ int) Upstream {
		cfg, _ := ordered.Get(alias)
		return Upstream{Alias: alias, Config: cfg}
	})...)
}

func (s *Service) getFromUpstream(
	ctx context.Context,
	req Request,
	requestRoute route,
	upstreamCfg config.UpstreamConfig,
	mode requestMode,
) (*Response, error) {
	if err := s.checkDependencyPolicy(requestRoute); err != nil {
		s.recordPolicyDeniedPull(ctx, req, requestRoute, err)
		return nil, err
	}
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, err
	}
	resp, cachedHit, cacheErr := s.responseFromCached(req, requestRoute, cached, cachedOK, mode)
	if cachedHit || cacheErr != nil {
		return resp, cacheErr
	}

	fillReq := upstreamFillRequest{
		ctx:      ctx,
		req:      req,
		route:    requestRoute,
		upstream: upstreamCfg,
		cached:   cached,
		cachedOK: cachedOK,
		mode:     mode,
	}
	if shouldCoalesceFill(req, requestRoute, mode) {
		return s.coalesceFill(fillReq)
	}

	return s.fetchUncached(fillReq)
}

type upstreamFillRequest struct {
	ctx      context.Context
	req      Request
	route    route
	upstream config.UpstreamConfig
	cached   storedResponse
	cachedOK bool
	mode     requestMode
}

func (s *Service) coalesceFill(fillReq upstreamFillRequest) (*Response, error) {
	resp, err := artifactcache.CoalesceFillWith(artifactcache.CoalesceRequest[*Response]{
		Context: fillReq.ctx,
		Tracker: s.fills,
		Key:     artifactKey(fillReq.route),
		Wait: func() (*Response, bool, error) {
			refreshed, ok, refreshErr := s.cached(fillReq.ctx, fillReq.route)
			if refreshErr != nil {
				return nil, true, refreshErr
			}
			cachedResp, cachedHit, cacheErr := s.responseFromCached(fillReq.req, fillReq.route, refreshed, ok, fillReq.mode)
			if cachedHit || cacheErr != nil {
				return cachedResp, true, cacheErr
			}
			return nil, false, nil
		},
		Fill: func() (*Response, error) {
			return s.fetchUncached(fillReq)
		},
	})
	if err != nil {
		return nil, wrapError(err, "coalesce npm artifact fill")
	}
	return resp, nil
}

func (s *Service) fetchUncached(fillReq upstreamFillRequest) (*Response, error) {
	fetched, err := s.fetch(fillReq.ctx, fillReq.upstream, fillReq.route.Alias, fillReq.route, fillReq.req.Method)
	if err != nil {
		return s.responseFromFetchError(fillReq.req, fillReq.route, fillReq.cached, fillReq.cachedOK, err, fillReq.mode)
	}
	return s.responseFromFetched(fillReq.ctx, fillReq.req, fillReq.route, fetched)
}

func (s *Service) refresh(ctx context.Context, req Request) (*Response, error) {
	requestRoute, err := parseRoute(req)
	if err != nil {
		return nil, err
	}
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("npm").
			With("alias", requestRoute.Alias).
			Errorf("npm upstream is not configured")
	}
	requestRoute.MetadataTTL = s.metadataTTL
	if requestRoute.MetadataTTL <= 0 {
		requestRoute.MetadataTTL = upstreamCfg.TagTTL
	}
	req.SkipPullRecord = true
	return s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeRefresh)
}

func (s *Service) fetch(ctx context.Context, cfg config.UpstreamConfig, upstreamAlias string, requestRoute route, method string) (*upstreamFetch, error) {
	endpoints := ecosystem.UpstreamEndpoints(ctx, s.metadata, ecosystem.NPM, upstreamAlias, cfg)
	var lastErr error
	for _, endpoint := range endpoints {
		requestURL := strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(requestRoute.UpstreamTail, "/")
		requestURL = urlWithQuery(requestURL, requestRoute.Query)
		resp, err := s.fetchURL(ctx, cfg, requestURL, method)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no npm upstream endpoint is configured")
	}
	return nil, wrapError(lastErr, "fetch npm upstream")
}

func (s *Service) fetchURL(ctx context.Context, cfg config.UpstreamConfig, requestURL, method string) (*upstreamFetch, error) {
	headers := http.Header{}
	headers.Set("User-Agent", "regimux/dev")
	resp, err := s.doFetch(ctx, cfg, requestURL, upstreamhttp.Request{
		Method:  requestMethod(method),
		URL:     requestURL,
		Headers: headers,
		Auth:    cfg.Auth,
	})
	if err != nil {
		return nil, wrapError(err, "send npm upstream request")
	}
	defer closeReadCloser(resp.Body, s.logger, "close npm upstream response body")
	body, err := materializeBody(resp.Body)
	if err != nil {
		return nil, err
	}
	return &upstreamFetch{
		status:     resp.Status,
		headers:    resp.Headers,
		body:       body,
		requestURL: requestURL,
	}, nil
}

func (s *Service) doFetch(ctx context.Context, cfg config.UpstreamConfig, baseURL string, req upstreamhttp.Request) (*upstreamhttp.Response, error) {
	if s.client != nil {
		resp, err := upstreamhttp.RawDo(ctx, s.client, req)
		if err != nil {
			return nil, wrapError(err, "send npm upstream raw request")
		}
		return resp, nil
	}
	client, err := s.clientFor(cfg, baseURL)
	if err != nil {
		return nil, err
	}
	resp, err := upstreamhttp.Do(ctx, client, req)
	if err != nil {
		return nil, wrapError(err, "send npm upstream clientx request")
	}
	return resp, nil
}

const npmMetadataMaxBytes int64 = 64 << 20

func (s *Service) prepareFetched(req Request, requestRoute route, fetched *upstreamFetch) (*upstreamFetch, error) {
	if fetched == nil || fetched.body == nil {
		return fetched, nil
	}
	if requestRoute.Kind != routeMetadata {
		return fetched, nil
	}
	body, err := upstreamhttp.ReadAllLimited(fetched.body, npmMetadataMaxBytes)
	if err != nil {
		return nil, wrapError(err, "read npm upstream body")
	}
	closeReadCloser(fetched.body, s.logger, "close npm upstream body")

	rewritten, changed, err := rewriteMetadata(body, localBase(requestPublicURL(s.publicURL, req.ProxyBaseURL), requestRoute.Alias))
	if err != nil {
		return nil, err
	}
	if !changed {
		return &upstreamFetch{
			status:     fetched.status,
			headers:    fetched.headers,
			body:       io.NopCloser(bytes.NewReader(body)),
			requestURL: fetched.requestURL,
		}, nil
	}
	headers := fetched.headers.Clone()
	headers.Del("Content-Length")
	return &upstreamFetch{
		status:     fetched.status,
		headers:    headers,
		body:       io.NopCloser(bytes.NewReader(rewritten)),
		requestURL: fetched.requestURL,
	}, nil
}

func (s *Service) storeFetchedResponse(ctx context.Context, req Request, requestRoute route, fetched *upstreamFetch) (*Response, error) {
	stored, err := s.store(ctx, requestRoute, fetched)
	if err != nil {
		return nil, err
	}
	return s.responseFromStored(req, requestRoute, storedResponse{
		digest:  stored.digest,
		size:    stored.size,
		headers: stored.headers,
		body:    stored.body,
	}, cacheMiss)
}

func (s *Service) responseFromFetchError(req Request, requestRoute route, cached storedResponse, cachedOK bool, err error, mode requestMode) (*Response, error) {
	if mode == requestModeRefresh {
		return nil, err
	}
	if cachedOK {
		return s.responseFromStored(req, requestRoute, cached, cacheStale)
	}
	return nil, err
}

func (s *Service) responseFromStored(req Request, requestRoute route, stored storedResponse, cacheStatus string) (*Response, error) {
	headers := cacheHeaders(stored.headers, stored.size)
	headers.Set(headerMirrorCache, cacheStatus)
	body := stored.body
	if requestMethod(req.Method) == http.MethodHead && body != nil {
		if err := body.Close(); err != nil {
			return nil, wrapError(err, "close cached npm proxy object for head")
		}
		body = http.NoBody
	}
	return &Response{
		Status:      http.StatusOK,
		Headers:     headers,
		Body:        body,
		ContentType: contentType(requestRoute, headers),
		Size:        stored.size,
		Cache:       cacheStatus,
	}, nil
}

func (s *Service) responseFromUpstream(req Request, requestRoute route, fetched *upstreamFetch) *Response {
	headers := cacheHeaders(fetched.headers, -1)
	headers.Set(headerMirrorCache, cacheMiss)
	body := fetched.body
	if requestMethod(req.Method) == http.MethodHead && body != nil {
		closeReadCloser(body, s.logger, "close npm upstream body for head")
		body = http.NoBody
	}
	return &Response{
		Status:      fetched.status,
		Headers:     headers,
		Body:        body,
		ContentType: contentType(requestRoute, headers),
		Size:        contentLength(headers),
		Cache:       cacheMiss,
	}
}

func (s *Service) upstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.NPMUpstream(alias)
}

func shouldPassThrough(req Request, status int) bool {
	return status < http.StatusOK ||
		status >= http.StatusMultipleChoices ||
		requestMethod(req.Method) == http.MethodHead
}

func shouldCoalesceFill(req Request, requestRoute route, mode requestMode) bool {
	return mode == requestModeClient &&
		requestMethod(req.Method) == http.MethodGet &&
		cacheable(requestRoute)
}
