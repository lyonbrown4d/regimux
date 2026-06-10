package pypi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
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
	req, err := http.NewRequestWithContext(ctx, methodOrGet(method), requestURL, http.NoBody)
	if err != nil {
		return nil, wrapError(err, "create pypi upstream request")
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	applyAuth(req, cfg.Auth)

	client, err := s.clientFor(cfg, requestURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, wrapError(err, "send pypi upstream request")
	}
	defer closeReadCloser(resp.Body, s.logger, "close pypi upstream response body")
	body, err := materializeHTTPBody(resp.Body)
	if err != nil {
		return nil, err
	}
	return &upstreamFetch{
		status:     resp.StatusCode,
		headers:    resp.Header.Clone(),
		body:       body,
		requestURL: requestURL,
	}, nil
}

func (s *Service) clientFor(cfg config.UpstreamConfig, baseURL string) (*http.Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	factory := s.factory
	if factory == nil {
		factory = clientfactory.New(s.logger)
	}
	client, err := factory.RawUpstreamHTTP(cfg, baseURL, "pypi.clientx")
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

func applyAuth(req *http.Request, cfg config.AuthConfig) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "basic":
		req.SetBasicAuth(cfg.Username, cfg.Password)
	case "bearer":
		if token := strings.TrimSpace(cfg.Token); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
}
