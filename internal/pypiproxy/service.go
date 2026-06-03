package pypiproxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
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
	client := deps.Client
	if client == nil {
		client = &http.Client{}
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
		client:    client,
		logger:    logger.With("component", "pypi-proxy"),
		publicURL: strings.TrimRight(deps.Config.Server.PublicURL, "/"),
		now:       now,
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("pypi-proxy").Errorf("service is nil")
	}
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	requestRoute.Query = strings.TrimSpace(req.Query)
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("pypi-proxy").With("alias", requestRoute.Alias).Errorf("pypi upstream is not configured")
	}
	return s.getFromUpstream(ctx, req, requestRoute, upstreamCfg)
}

func (s *Service) getFromUpstream(ctx context.Context, req Request, requestRoute Route, upstreamCfg config.UpstreamConfig) (*Response, error) {
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, err
	}
	if cachedOK && !cached.expired {
		return s.responseFromStored(req, requestRoute, cached, cacheHit)
	}

	fetched, err := s.fetch(ctx, upstreamCfg, requestRoute, req.Method)
	if err != nil {
		if cachedOK {
			return s.responseFromStored(req, requestRoute, cached, cacheStale)
		}
		return nil, err
	}
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

func (s *Service) upstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.PyPIUpstream(alias)
}

func (s *Service) Upstreams() []Upstream {
	if s == nil {
		return nil
	}
	ordered := s.cfg.OrderedPyPIUpstreams()
	out := make([]Upstream, 0, ordered.Len())
	ordered.Range(func(alias string, cfg config.UpstreamConfig) bool {
		out = append(out, Upstream{Alias: alias, Config: cfg})
		return true
	})
	return out
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
