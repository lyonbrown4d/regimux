package goproxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/samber/oops"
)

const (
	defaultMetadataTTL = 5 * time.Minute

	headerMirrorCache = "X-Mirror-Cache"
	cacheHit          = "hit"
	cacheMiss         = "miss"
	cacheStale        = "stale"
)

type ServiceDependencies struct {
	Config   config.Config
	Metadata meta.Store
	Objects  object.Store
	Logger   *slog.Logger
}

type Service struct {
	cfg      config.Config
	metadata meta.Store
	objects  object.Store
	client   *http.Client
	logger   *slog.Logger
}

type Request struct {
	Alias          string
	Tail           string
	Method         string
	SkipPullRecord bool
}

type Response struct {
	Status      int
	Headers     http.Header
	Body        io.ReadCloser
	ContentType string
	Size        int64
	Cache       string
}

type upstreamFetch struct {
	status  int
	headers http.Header
	body    io.ReadCloser
}

type Upstream struct {
	Alias  string
	Config config.UpstreamConfig
}

type goUpstream struct {
	alias string
	cfg   config.UpstreamConfig
}

type storedResponse struct {
	digest  string
	size    int64
	headers http.Header
	body    io.ReadCloser
	expired bool
}

func NewService(deps ServiceDependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:      deps.Config,
		metadata: deps.Metadata,
		objects:  deps.Objects,
		client:   &http.Client{},
		logger:   logger.With("component", "go-proxy"),
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("go-proxy").Errorf("service is nil")
	}
	if strings.TrimSpace(req.Alias) == "" {
		parsed, err := parseRootRoute(req.Tail)
		if err != nil {
			return nil, err
		}
		return s.getFromUpstreams(ctx, req, parsed, s.goUpstreams(), true)
	}

	parsed, err := parseRoute(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	upstreamCfg, ok := s.goUpstream(parsed.Alias)
	if !ok {
		return nil, oops.In("go-proxy").With("alias", parsed.Alias).Errorf("go upstream is not configured")
	}
	resp, err := s.getFromUpstreams(ctx, req, parsed, collectionlist.NewList(goUpstream{alias: parsed.Alias, cfg: upstreamCfg}), false)
	s.recordPull(ctx, req, parsed, resp, err)
	return resp, err
}

func (s *Service) getFromUpstreams(ctx context.Context, req Request, baseRoute route, upstreams *collectionlist.List[goUpstream], fallback bool) (*Response, error) {
	if upstreams == nil || upstreams.Len() == 0 {
		return nil, oops.In("go-proxy").Errorf("go upstream is not configured")
	}
	total := upstreams.Len()
	var result *Response
	var lastErr error
	upstreams.Range(func(i int, upstream goUpstream) bool {
		requestRoute := routeForUpstream(baseRoute, upstream.alias)
		resp, err := s.getFromUpstream(ctx, req, requestRoute, upstream.cfg, upstream.alias)
		if err != nil {
			lastErr = err
			if canFallback(fallback, i, total) {
				return true
			}
			lastErr = err
			return false
		}
		if canFallbackResponse(resp, fallback, i, total) {
			closeResponseBody(resp)
			return true
		}
		result = resp
		return false
	})
	if result != nil {
		return result, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, oops.In("go-proxy").Errorf("go upstream did not return module content")
}

func (s *Service) getFromUpstream(ctx context.Context, req Request, requestRoute route, upstreamCfg config.UpstreamConfig, upstreamAlias string) (*Response, error) {
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, err
	}
	if cacheFresh(cached, cachedOK) {
		return s.responseFromStored(req, cached, cacheHit)
	}

	fetched, err := s.fetch(ctx, upstreamCfg, upstreamAlias, requestRoute, req.Method)
	if err != nil {
		return s.responseFromFetchError(req, cached, cachedOK, err)
	}
	if shouldPassThrough(req, requestRoute, fetched.status) {
		return s.responseFromUpstream(req, fetched), nil
	}
	return s.storeFetchedResponse(ctx, req, requestRoute, fetched)
}

func routeForUpstream(baseRoute route, alias string) route {
	baseRoute.Alias = alias
	return baseRoute
}

func canFallback(fallback bool, index, total int) bool {
	return fallback && index+1 < total
}

func canFallbackResponse(resp *Response, fallback bool, index, total int) bool {
	return resp != nil && canFallback(fallback, index, total) && fallbackStatus(resp.Status)
}

func cacheFresh(cached storedResponse, ok bool) bool {
	return ok && !cached.expired
}

func (s *Service) responseFromFetchError(req Request, cached storedResponse, cachedOK bool, err error) (*Response, error) {
	if cachedOK {
		return s.responseFromStored(req, cached, cacheStale)
	}
	return nil, err
}

func shouldPassThrough(req Request, requestRoute route, status int) bool {
	return status < http.StatusOK ||
		status >= http.StatusMultipleChoices ||
		methodOr(req.Method, http.MethodGet) == http.MethodHead ||
		!routeCacheable(requestRoute)
}

func (s *Service) storeFetchedResponse(ctx context.Context, req Request, requestRoute route, fetched *upstreamFetch) (*Response, error) {
	stored, err := s.store(ctx, requestRoute, fetched)
	if err != nil {
		return nil, err
	}
	return s.responseFromStored(req, storedResponse{
		digest:  stored.digest,
		size:    stored.size,
		headers: stored.headers,
		body:    stored.body,
	}, cacheMiss)
}

func (s *Service) recordPull(ctx context.Context, req Request, requestRoute route, resp *Response, err error) {
	if !s.shouldRecordPull(req, requestRoute, resp, err) {
		return
	}
	key := goPullKey(requestRoute)
	s.recordPullKey(ctx, key, resp.Cache == cacheMiss)
}

func (s *Service) shouldRecordPull(req Request, requestRoute route, resp *Response, err error) bool {
	return s != nil &&
		s.metadata != nil &&
		!req.SkipPullRecord &&
		err == nil &&
		resp != nil &&
		routeCacheable(requestRoute) &&
		resp.Status >= http.StatusOK &&
		resp.Status < http.StatusMultipleChoices
}

func goPullKey(requestRoute route) meta.PullKey {
	return meta.PullKey{
		Alias:      ecosystem.ScopedAlias(ecosystem.Go, requestRoute.Alias),
		Repository: requestRoute.Module,
		Reference:  requestRoute.Reference,
	}
}

func (s *Service) recordPullKey(ctx context.Context, key meta.PullKey, upstream bool) {
	now := time.Now().UTC()
	if _, recordErr := s.metadata.RecordPull(ctx, key, now); recordErr != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "record go proxy pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
	}
	if !upstream {
		return
	}
	if _, recordErr := s.metadata.RecordUpstreamPull(ctx, key, now); recordErr != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "record go proxy upstream pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
	}
}

func (s *Service) goUpstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.GoUpstream(alias)
}

func (s *Service) Upstreams() *collectionlist.List[Upstream] {
	if s == nil {
		return collectionlist.NewList[Upstream]()
	}
	upstreams := s.goUpstreams()
	return collectionlist.MapList(upstreams, func(_ int, upstream goUpstream) Upstream {
		return Upstream{Alias: upstream.alias, Config: upstream.cfg}
	})
}

func (s *Service) goUpstreams() *collectionlist.List[goUpstream] {
	ordered := s.cfg.OrderedGoUpstreams()
	if ordered.Len() == 0 {
		return collectionlist.NewList[goUpstream]()
	}
	out := collectionlist.NewListWithCapacity[goUpstream](ordered.Len())
	ordered.Range(func(alias string, cfg config.UpstreamConfig) bool {
		out.Add(goUpstream{alias: alias, cfg: cfg})
		return true
	})
	return preferGoAlias(out, "default")
}

func preferGoAlias(upstreams *collectionlist.List[goUpstream], alias string) *collectionlist.List[goUpstream] {
	if upstreams == nil || upstreams.Len() == 0 {
		return collectionlist.NewList[goUpstream]()
	}
	if alias == "" {
		return upstreams
	}

	preferredIndex := -1
	var preferred goUpstream
	upstreams.Range(func(index int, upstream goUpstream) bool {
		if upstream.alias == alias && preferredIndex == -1 {
			preferredIndex = index
			preferred = upstream
		}
		return true
	})
	if preferredIndex <= 0 {
		return upstreams
	}
	preferredUpstreams := collectionlist.NewListWithCapacity[goUpstream](upstreams.Len())
	preferredUpstreams.Add(preferred)
	upstreams.Range(func(index int, upstream goUpstream) bool {
		if index == preferredIndex {
			return true
		}
		preferredUpstreams.Add(upstream)
		return true
	})
	return preferredUpstreams
}
