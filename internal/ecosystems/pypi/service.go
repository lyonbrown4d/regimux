package pypi

import (
	"bytes"
	"context"
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
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
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
	return &Service{
		cfg:       deps.Config,
		cache:     cache,
		metadata:  deps.Metadata,
		client:    deps.Client,
		factory:   factory,
		logger:    logger.With("component", "pypi"),
		publicURL: strings.TrimRight(deps.Config.Server.PublicURL, "/"),
		now:       now,
		events:    deps.Events,
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("pypi").Errorf("service is nil")
	}
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	requestRoute.Query = strings.TrimSpace(req.Query)
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("pypi").With("alias", requestRoute.Alias).Errorf("pypi upstream is not configured")
	}
	resp, err := s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeClient)
	s.recordPull(ctx, req, requestRoute, resp, err)
	return resp, err
}

func (s *Service) getFromUpstream(ctx context.Context, req Request, requestRoute Route, upstreamCfg config.UpstreamConfig, mode requestMode) (*Response, error) {
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, err
	}
	resp, cachedHit, cacheErr := s.responseFromCached(req, requestRoute, cached, cachedOK, mode)
	if cachedHit || cacheErr != nil {
		return resp, cacheErr
	}

	fetched, err := s.fetch(ctx, upstreamCfg, requestRoute.Alias, requestRoute, req.Method)
	if err != nil {
		return s.responseFromFetchError(req, requestRoute, cached, cachedOK, err, mode)
	}
	return s.responseFromFetched(ctx, req, requestRoute, fetched)
}

func (s *Service) responseFromCached(req Request, requestRoute Route, cached storedResponse, cachedOK bool, mode requestMode) (*Response, bool, error) {
	if mode == requestModeRefresh || !cachedOK {
		return nil, false, nil
	}
	if cached.expired {
		resp, err := s.responseFromStored(req, requestRoute, cached, cacheStale)
		return resp, true, err
	}
	resp, err := s.responseFromStored(req, requestRoute, cached, cacheHit)
	return resp, true, err
}

func (s *Service) responseFromFetchError(req Request, requestRoute Route, cached storedResponse, cachedOK bool, err error, mode requestMode) (*Response, error) {
	if mode == requestModeRefresh {
		return nil, err
	}
	if cachedOK {
		return s.responseFromStored(req, requestRoute, cached, cacheStale)
	}
	return nil, err
}

func (s *Service) responseFromFetched(ctx context.Context, req Request, requestRoute Route, fetched *upstreamFetch) (*Response, error) {
	if shouldPassThrough(req, fetched.status) {
		return s.responseFromUpstream(req, requestRoute, fetched), nil
	}
	prepared, err := s.prepareFetched(req, requestRoute, fetched)
	if err != nil {
		return nil, err
	}
	stored, err := s.store(ctx, requestRoute, prepared)
	if err != nil {
		return nil, err
	}
	return s.responseFromStored(req, requestRoute, stored, cacheMiss)
}

func (s *Service) refresh(ctx context.Context, req Request) (*Response, error) {
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	requestRoute.Query = strings.TrimSpace(req.Query)
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("pypi").With("alias", requestRoute.Alias).Errorf("pypi upstream is not configured")
	}
	req.SkipPullRecord = true
	return s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeRefresh)
}

func (s *Service) prepareFetched(req Request, requestRoute Route, fetched *upstreamFetch) (*upstreamFetch, error) {
	if fetched == nil || fetched.body == nil || requestRoute.Kind != RouteSimple {
		return fetched, nil
	}
	body, err := io.ReadAll(fetched.body)
	if err != nil {
		return nil, wrapError(err, "read pypi upstream body")
	}
	closeReadCloser(fetched.body, s.logger, "close pypi upstream body")

	rewritten := rewriteSimpleIndexLinks(body, requestRoute.Alias, fetched.requestURL, requestPublicURL(s.publicURL, req.PublicURL))
	if bytesEqual(body, rewritten) {
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

func (s *Service) recordPull(ctx context.Context, req Request, requestRoute Route, resp *Response, err error) {
	if !s.shouldRecordPull(req, resp, err) {
		return
	}
	key := pypiPullKey(requestRoute)
	s.recordPullKey(ctx, key, resp.Cache == cacheMiss)
	s.publishArtifactPulled(ctx, requestRoute, resp)
}

func (s *Service) shouldRecordPull(req Request, resp *Response, err error) bool {
	return s != nil &&
		s.metadata != nil &&
		!req.SkipPullRecord &&
		err == nil &&
		resp != nil &&
		resp.Status >= http.StatusOK &&
		resp.Status < http.StatusMultipleChoices
}

func pypiPullKey(requestRoute Route) meta.PullKey {
	return meta.PullKey{
		Alias:      ecosystem.ScopedAlias(ecosystem.PyPI, requestRoute.Alias),
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
	}
}

func (s *Service) recordPullKey(ctx context.Context, key meta.PullKey, upstream bool) {
	now := s.now()
	if _, recordErr := s.metadata.RecordPull(ctx, key, now); recordErr != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "record pypi proxy pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
	}
	if !upstream {
		return
	}
	if _, recordErr := s.metadata.RecordUpstreamPull(ctx, key, now); recordErr != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "record pypi proxy upstream pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
	}
}

func (s *Service) publishArtifactPulled(ctx context.Context, requestRoute Route, resp *Response) {
	if s == nil || s.events == nil || resp == nil || requestRoute.Kind != RouteSimple {
		return
	}
	if err := events.Publish(ctx, s.events, events.ArtifactPulled{
		Ecosystem:  ecosystem.PyPI,
		Kind:       string(RouteSimple),
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
		Status:     resp.Cache,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish pypi proxy artifact pulled event failed", "alias", requestRoute.Alias, "repository", requestRoute.Repository, "reference", requestRoute.Reference, "error", err)
	}
}

func (s *Service) upstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.PyPIUpstream(alias)
}

func (s *Service) Upstreams() *collectionlist.List[Upstream] {
	if s == nil {
		return collectionlist.NewList[Upstream]()
	}
	ordered := s.cfg.OrderedPyPIUpstreams()
	return collectionlist.MapList(collectionlist.NewList(ordered.Keys()...), func(_ int, alias string) Upstream {
		cfg, _ := ordered.Get(alias)
		return Upstream{Alias: alias, Config: cfg}
	})
}

func (s *Service) upstreamSimpleTTL(alias string) time.Duration {
	cfg, ok := s.upstream(alias)
	if ok && cfg.TagTTL > 0 {
		return cfg.TagTTL
	}
	return defaultSimpleTTL
}

func routeTTL(requestRoute Route, simpleTTL time.Duration) time.Duration {
	if requestRoute.Kind != RouteSimple {
		return 0
	}
	if simpleTTL > 0 {
		return simpleTTL
	}
	return defaultSimpleTTL
}
