package dist

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
	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func (s *Service) fetch(ctx context.Context, cfg config.UpstreamConfig, requestRoute Route, req Request) (*upstreamFetch, error) {
	endpoints := ecosystem.UpstreamEndpoints(ctx, s.metadata, ecosystem.Dist, requestRoute.Alias, cfg)
	var lastErr error
	for i, endpoint := range endpoints {
		requestURL := strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(requestRoute.UpstreamTail, "/")
		resp, err := s.fetchURL(ctx, cfg, requestURL, req)
		if err == nil {
			if shouldTryNextDistEndpoint(resp.status, i, len(endpoints)) {
				closeReadCloser(resp.body, s.logger, "close fallback dist upstream body")
				continue
			}
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no dist upstream endpoint is configured")
	}
	return nil, wrapError(lastErr, "fetch dist upstream")
}

func shouldTryNextDistEndpoint(status, index, total int) bool {
	return index+1 < total && canFallbackDistStatus(status)
}

func canFallbackDistStatus(status int) bool {
	switch status {
	case http.StatusNotFound, http.StatusGone, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	default:
		return status >= http.StatusInternalServerError
	}
}

func (s *Service) fetchURL(ctx context.Context, cfg config.UpstreamConfig, requestURL string, distReq Request) (*upstreamFetch, error) {
	transferCtx, cancel := context.WithCancel(ctx)
	resp, err := s.fetchDistAttempt(transferCtx, cfg, requestURL, distReq)
	if err != nil {
		cancel()
		return nil, wrapError(err, "send dist upstream request")
	}
	resp.Body = transfer.CancelOnClose(s.recoverDistHTTP(transferCtx, cfg, requestURL, distReq, resp), cancel)
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			s.logger.Warn("close dist upstream response body failed", "error", closeErr)
		}
	}()
	if resp.Status == http.StatusPartialContent && !transfer.MatchesIfRange(distReq.IfRange, resp.Headers) {
		distReq.Range, distReq.IfRange = "", ""
		closeReadCloser(resp.Body, s.logger, "close dist mismatched If-Range response")
		return s.fetchURL(ctx, cfg, requestURL, distReq)
	}
	body, err := materializeHTTPBody(resp.Body)
	if err != nil {
		return nil, err
	}
	return selectDistRange(distReq, resp, body)
}

func (s *Service) doFetch(ctx context.Context, cfg config.UpstreamConfig, baseURL string, req upstreamhttp.Request) (*upstreamhttp.Response, error) {
	if s.client != nil {
		resp, err := upstreamhttp.RawDo(ctx, s.client, req)
		if err != nil {
			return nil, wrapError(err, "send dist upstream raw request")
		}
		return resp, nil
	}
	client, err := s.clientFor(cfg, baseURL)
	if err != nil {
		return nil, err
	}
	resp, err := upstreamhttp.Do(ctx, client, req)
	if err != nil {
		return nil, wrapError(err, "send dist upstream clientx request")
	}
	return resp, nil
}

func (s *Service) clientFor(cfg config.UpstreamConfig, baseURL string) (clienthttp.Client, error) {
	factory := s.factory
	client, err := upstreamhttp.NewClient(factory, cfg, baseURL, "dist.clientx")
	if err != nil {
		return nil, wrapError(err, "create dist upstream client")
	}
	return client, nil
}

func materializeHTTPBody(body io.ReadCloser) (io.ReadCloser, error) {
	materialized, err := spool.MaterializeReadCloser(body, "regimux-dist-upstream-*")
	if err != nil {
		return nil, wrapError(err, "materialize dist upstream body")
	}
	return materialized, nil
}
