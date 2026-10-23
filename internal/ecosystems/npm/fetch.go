package npm

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func (s *Service) fetch(ctx context.Context, cfg config.UpstreamConfig, upstreamAlias string, requestRoute route, method string) (*upstreamFetch, error) {
	endpoints := ecosystem.UpstreamEndpoints(ctx, s.metadata, ecosystem.NPM, upstreamAlias, cfg)
	var lastErr error
	for _, endpoint := range endpoints {
		requestURL := strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(requestRoute.UpstreamTail, "/")
		requestURL = urlWithQuery(requestURL, requestRoute.Query)
		resp, err := s.fetchURL(ctx, cfg, endpoint, requestURL, method)
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

func (s *Service) fetchURL(ctx context.Context, cfg config.UpstreamConfig, registryBaseURL, requestURL, method string) (*upstreamFetch, error) {
	headers := http.Header{}
	headers.Set("User-Agent", "regimux/dev")
	headers.Set("Accept-Encoding", "identity")
	attemptCtx, cancel := context.WithCancel(ctx)
	resp, err := s.doFetch(attemptCtx, cfg, requestURL, upstreamhttp.Request{
		Method:  requestMethod(method),
		URL:     requestURL,
		Headers: headers,
		Auth:    cfg.Auth,
	})
	if err != nil {
		cancel()
		return nil, wrapError(err, "send npm upstream request")
	}
	upstreamBody := upstreamhttp.WithIdleTimeout(resp.Body, cancel, upstreamhttp.DefaultBodyIdleTimeout)
	if resp.Status == http.StatusOK {
		upstreamBody = transfer.HTTPRecovery(ctx, requestMethod(method), resp.Headers, upstreamBody, "", func(retryCtx context.Context, retryHeaders http.Header) (*transfer.Response, error) {
			requestHeaders := headers.Clone()
			mergeNPMHeaders(requestHeaders, retryHeaders)
			continued, fetchErr := upstreamhttp.OpenStream(retryCtx, cfg.HTTP.AttemptTimeout, upstreamhttp.DefaultBodyIdleTimeout,
				func(attemptCtx context.Context) (*upstreamhttp.Response, error) {
					return s.doFetch(attemptCtx, cfg, requestURL, upstreamhttp.Request{
						Method: requestMethod(method), URL: requestURL, Headers: requestHeaders, Auth: cfg.Auth,
					})
				})
			if fetchErr != nil {
				return nil, wrapError(fetchErr, "resume npm upstream request")
			}
			return &transfer.Response{Status: continued.Status, Headers: continued.Headers, Body: continued.Body, Size: contentLength(continued.Headers)}, nil
		})
	}
	defer closeReadCloser(upstreamBody, s.logger, "close npm upstream response body")
	materialized, err := materializeBody(upstreamBody)
	if err != nil {
		return nil, err
	}
	return &upstreamFetch{
		status:          resp.Status,
		headers:         resp.Headers,
		body:            materialized,
		requestURL:      requestURL,
		registryBaseURL: registryBaseURL,
	}, nil
}

func mergeNPMHeaders(target, source http.Header) {
	for key, values := range source {
		target[key] = append([]string(nil), values...)
	}
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
