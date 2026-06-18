package golang

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	clienthttp "github.com/arcgolabs/clientx/http"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/spool"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func (s *Service) fetch(ctx context.Context, cfg config.UpstreamConfig, upstreamAlias string, requestRoute route, method string) (*upstreamFetch, error) {
	endpoints := ecosystem.UpstreamEndpoints(ctx, s.metadata, ecosystem.Go, upstreamAlias, cfg)
	var lastErr error
	for _, endpoint := range endpoints {
		resp, err := s.fetchEndpoint(ctx, cfg, endpoint, requestRoute.Tail, method)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no go upstream endpoint is configured")
	}
	return nil, wrapError(lastErr, "fetch go proxy upstream")
}

func (s *Service) fetchEndpoint(ctx context.Context, cfg config.UpstreamConfig, endpoint, tail, method string) (*upstreamFetch, error) {
	requestURL := strings.TrimRight(endpoint, "/") + "/" + tail
	headers := http.Header{}
	headers.Set("User-Agent", "regimux/dev")
	client, err := s.clientFor(cfg, endpoint)
	if err != nil {
		return nil, err
	}
	resp, err := upstreamhttp.Do(ctx, client, upstreamhttp.Request{
		Method:  methodOr(method, http.MethodGet),
		URL:     requestURL,
		Headers: headers,
		Auth:    cfg.Auth,
	})
	if err != nil {
		return nil, wrapError(err, "send go proxy upstream request")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			s.logger.Warn("close go proxy upstream response body failed", "error", closeErr)
		}
	}()
	body, err := materializeHTTPBody(resp.Body)
	if err != nil {
		return nil, err
	}
	return &upstreamFetch{
		status:  resp.Status,
		headers: resp.Headers,
		body:    body,
	}, nil
}

func (s *Service) clientFor(cfg config.UpstreamConfig, baseURL string) (clienthttp.Client, error) {
	factory := s.factory
	client, err := upstreamhttp.NewClient(factory, cfg, baseURL, "go.clientx")
	if err != nil {
		return nil, wrapError(err, "create go proxy upstream client")
	}
	return client, nil
}

func materializeHTTPBody(body io.ReadCloser) (io.ReadCloser, error) {
	materialized, err := spool.MaterializeReadCloser(body, "regimux-go-upstream-*")
	if err != nil {
		return nil, wrapError(err, "materialize go proxy upstream body")
	}
	return materialized, nil
}
