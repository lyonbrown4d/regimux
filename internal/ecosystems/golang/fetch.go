package golang

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
	if body == nil {
		return http.NoBody, nil
	}
	tmp, err := os.CreateTemp("", "regimux-go-upstream-*")
	if err != nil {
		return nil, wrapError(err, "create go proxy upstream temp file")
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, body); err != nil {
		return nil, closeAndRemoveTemp(tmp, name, err, "copy go proxy upstream body")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, closeAndRemoveTemp(tmp, name, err, "rewind go proxy upstream temp file")
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
