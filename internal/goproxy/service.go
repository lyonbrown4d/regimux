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
	var lastErr error
	for i := range total {
		upstream, ok := upstreams.Get(i)
		if !ok {
			continue
		}
		requestRoute := routeForUpstream(baseRoute, upstream.alias)
		resp, err := s.getFromUpstream(ctx, req, requestRoute, upstream.cfg, upstream.alias)
		if s.shouldFallbackFromResponse(resp, err, fallback, i, total) {
			lastErr = err
			continue
		}
		if err != nil {
			return nil, err
		}
		return resp, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, oops.In("go-proxy").Errorf("go upstream did not return module content")
}

func (s *Service) shouldFallbackFromResponse(resp *Response, err error, fallback bool, index, total int) bool {
	if err != nil {
		return canFallback(fallback, index, total)
	}
	if canFallbackResponse(resp, fallback, index, total) {
		closeResponseBody(resp)
		return true
	}
	return false
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
