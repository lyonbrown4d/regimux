package dist

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	clienthttp "github.com/arcgolabs/clientx/http"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

type tempBody struct {
	*os.File
	name string
}

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
	headers := http.Header{}
	headers.Set("User-Agent", defaultUserAgent)
	if rangeHeader := strings.TrimSpace(distReq.Range); rangeHeader != "" && methodOrGet(distReq.Method) == http.MethodGet {
		headers.Set("Range", rangeHeader)
	}
	resp, err := s.doFetch(ctx, cfg, requestURL, upstreamhttp.Request{
		Method:  methodOrGet(distReq.Method),
		URL:     requestURL,
		Headers: headers,
		Auth:    cfg.Auth,
	})
	if err != nil {
		return nil, wrapError(err, "send dist upstream request")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			s.logger.Warn("close dist upstream response body failed", "error", closeErr)
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
	if body == nil {
		return http.NoBody, nil
	}
	tmp, err := os.CreateTemp("", "regimux-dist-upstream-*")
	if err != nil {
		return nil, wrapError(err, "create dist upstream temp file")
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, body); err != nil {
		return nil, closeAndRemoveTemp(tmp, name, err, "copy dist upstream body")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, closeAndRemoveTemp(tmp, name, err, "rewind dist upstream temp file")
	}
	return &tempBody{File: tmp, name: name}, nil
}

func (t *tempBody) Close() error {
	if t == nil || t.File == nil {
		return nil
	}
	closeErr := t.File.Close()
	removeErr := os.Remove(t.name)
	return errors.Join(closeErr, removeErr)
}

func closeAndRemoveTemp(file *os.File, name string, err error, message string) error {
	closeErr := file.Close()
	removeErr := os.Remove(name)
	return wrapError(errors.Join(err, closeErr, removeErr), message)
}
