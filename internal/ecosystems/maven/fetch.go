package maven

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
	endpoints := ecosystem.UpstreamEndpoints(ctx, s.metadata, ecosystem.Maven, upstreamAlias, cfg)
	var lastErr error
	for _, endpoint := range endpoints {
		requestURL := strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(requestRoute.UpstreamTail, "/")
		requestURL = urlWithQuery(requestURL, requestRoute.Query)
		resp, err := s.fetchURL(ctx, cfg, requestURL, method)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no maven upstream endpoint is configured")
	}
	return nil, wrapError(lastErr, "fetch maven upstream")
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
		return nil, wrapError(err, "send maven upstream request")
	}
	defer closeReadCloser(resp.Body, s.logger, "close maven upstream response body")
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
			return nil, wrapError(err, "send maven upstream raw request")
		}
		return resp, nil
	}
	client, err := s.clientFor(cfg, baseURL)
	if err != nil {
		return nil, err
	}
	resp, err := upstreamhttp.Do(ctx, client, req)
	if err != nil {
		return nil, wrapError(err, "send maven upstream clientx request")
	}
	return resp, nil
}

func (s *Service) clientFor(cfg config.UpstreamConfig, baseURL string) (clienthttp.Client, error) {
	factory := s.factory
	client, err := upstreamhttp.NewClient(factory, cfg, baseURL, "maven.clientx")
	if err != nil {
		return nil, wrapError(err, "create maven upstream client")
	}
	return client, nil
}

func materializeHTTPBody(body io.ReadCloser) (io.ReadCloser, error) {
	if body == nil {
		return http.NoBody, nil
	}
	tmp, err := os.CreateTemp("", "regimux-maven-upstream-*")
	if err != nil {
		return nil, wrapError(err, "create maven upstream temp file")
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, body); err != nil {
		return nil, closeAndRemoveTemp(tmp, name, err, "copy maven upstream body")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, closeAndRemoveTemp(tmp, name, err, "rewind maven upstream temp file")
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
