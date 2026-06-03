package goproxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
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
	Alias  string
	Tail   string
	Method string
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
	return s.getFromUpstreams(ctx, req, parsed, []goUpstream{{alias: parsed.Alias, cfg: upstreamCfg}}, false)
}

func (s *Service) getFromUpstreams(ctx context.Context, req Request, baseRoute route, upstreams []goUpstream, fallback bool) (*Response, error) {
	if len(upstreams) == 0 {
		return nil, oops.In("go-proxy").Errorf("go upstream is not configured")
	}
	var lastErr error
	for i := range upstreams {
		upstream := upstreams[i]
		requestRoute := routeForUpstream(baseRoute, upstream.alias)
		resp, err := s.getFromUpstream(ctx, req, requestRoute, upstream.cfg)
		if err != nil {
			lastErr = err
			if canFallback(fallback, i, len(upstreams)) {
				continue
			}
			return nil, err
		}
		if canFallbackResponse(resp, fallback, i, len(upstreams)) {
			closeResponseBody(resp)
			continue
		}
		return resp, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, oops.In("go-proxy").Errorf("go upstream did not return module content")
}

func (s *Service) getFromUpstream(ctx context.Context, req Request, requestRoute route, upstreamCfg config.UpstreamConfig) (*Response, error) {
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, err
	}
	if cacheFresh(cached, cachedOK) {
		return s.responseFromStored(req, cached, cacheHit)
	}

	fetched, err := s.fetch(ctx, upstreamCfg, requestRoute, req.Method)
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

func (s *Service) goUpstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.GoUpstream(alias)
}

func (s *Service) Upstreams() []Upstream {
	if s == nil {
		return nil
	}
	upstreams := s.goUpstreams()
	out := make([]Upstream, 0, len(upstreams))
	for i := range upstreams {
		out = append(out, Upstream{Alias: upstreams[i].alias, Config: upstreams[i].cfg})
	}
	return out
}

func (s *Service) goUpstreams() []goUpstream {
	ordered := s.cfg.OrderedGoUpstreams()
	if ordered.Len() == 0 {
		return nil
	}
	out := make([]goUpstream, 0, ordered.Len())
	ordered.Range(func(alias string, cfg config.UpstreamConfig) bool {
		out = append(out, goUpstream{alias: alias, cfg: cfg})
		return true
	})
	return preferGoAlias(out, "default")
}

func preferGoAlias(upstreams []goUpstream, alias string) []goUpstream {
	for i := range upstreams {
		if upstreams[i].alias != alias {
			continue
		}
		if i == 0 {
			return upstreams
		}
		preferred := upstreams[i]
		copy(upstreams[1:i+1], upstreams[0:i])
		upstreams[0] = preferred
		return upstreams
	}
	return upstreams
}
