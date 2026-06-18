package pypi

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

func (s *Service) fetch(ctx context.Context, cfg config.UpstreamConfig, upstreamAlias string, requestRoute Route, method string) (*upstreamFetch, error) {
	if requestRoute.DirectURL != "" {
		return s.fetchURL(ctx, cfg, urlWithQuery(requestRoute.DirectURL, requestRoute.Query), method)
	}

	endpoints := ecosystem.UpstreamEndpoints(ctx, s.metadata, ecosystem.PyPI, upstreamAlias, cfg)
	var lastErr error
	for _, endpoint := range endpoints {
		upstreamTail := upstreamTail(endpoint, requestRoute)
		requestURL := strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(upstreamTail, "/")
		requestURL = urlWithQuery(requestURL, requestRoute.Query)
		resp, err := s.fetchURL(ctx, cfg, requestURL, method)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no pypi upstream endpoint is configured")
	}
	return nil, wrapError(lastErr, "fetch pypi proxy upstream")
}

func upstreamTail(endpoint string, requestRoute Route) string {
	if requestRoute.Kind == RouteSimple && strings.HasSuffix(strings.TrimRight(endpoint, "/"), "/simple") {
		return strings.TrimPrefix(requestRoute.UpstreamTail, "simple/")
	}
	return requestRoute.UpstreamTail
}

func (s *Service) fetchURL(ctx context.Context, cfg config.UpstreamConfig, requestURL, method string) (*upstreamFetch, error) {
	headers := http.Header{}
	headers.Set("User-Agent", defaultUserAgent)
	resp, err := s.doFetch(ctx, cfg, requestURL, upstreamhttp.Request{
		Method:  methodOrGet(method),
		URL:     requestURL,
		Headers: headers,
		Auth:    cfg.Auth,
	})
	if err != nil {
		return nil, wrapError(err, "send pypi upstream request")
	}
	defer closeReadCloser(resp.Body, s.logger, "close pypi upstream response body")
	body, err := materializeHTTPBody(resp.Body)
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
			return nil, wrapError(err, "send pypi upstream raw request")
		}
		return resp, nil
	}
	client, err := s.clientFor(cfg, baseURL)
	if err != nil {
		return nil, err
	}
	resp, err := upstreamhttp.Do(ctx, client, req)
	if err != nil {
		return nil, wrapError(err, "send pypi upstream clientx request")
	}
	return resp, nil
}

func (s *Service) clientFor(cfg config.UpstreamConfig, baseURL string) (clienthttp.Client, error) {
	factory := s.factory
	client, err := upstreamhttp.NewClient(factory, cfg, baseURL, "pypi.clientx")
	if err != nil {
		return nil, wrapError(err, "create pypi upstream client")
	}
	return client, nil
}

func materializeHTTPBody(body io.ReadCloser) (io.ReadCloser, error) {
	if body == nil {
		return http.NoBody, nil
	}
	tmp, err := os.CreateTemp("", "regimux-pypi-upstream-*")
	if err != nil {
		return nil, wrapError(err, "create pypi upstream temp file")
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, body); err != nil {
		return nil, closeAndRemoveTemp(tmp, name, err, "copy pypi upstream body")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, closeAndRemoveTemp(tmp, name, err, "rewind pypi upstream temp file")
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

func urlWithQuery(rawURL, rawQuery string) string {
	rawQuery = strings.TrimPrefix(strings.TrimSpace(rawQuery), "?")
	if rawQuery == "" {
		return rawURL
	}
	if strings.Contains(rawURL, "?") {
		return rawURL + "&" + rawQuery
	}
	return rawURL + "?" + rawQuery
}
