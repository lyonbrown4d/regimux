package maven

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
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
	req, err := http.NewRequestWithContext(ctx, methodOrGet(method), requestURL, http.NoBody)
	if err != nil {
		return nil, wrapError(err, "create maven upstream request")
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	applyAuth(req, cfg.Auth)

	client := s.client
	if cfg.HTTP.Timeout > 0 {
		client = &http.Client{Timeout: cfg.HTTP.Timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, wrapError(err, "send maven upstream request")
	}
	defer closeReadCloser(resp.Body, s.logger, "close maven upstream response body")
	body, err := materializeHTTPBody(resp.Body)
	if err != nil {
		return nil, err
	}
	return &upstreamFetch{
		status:  resp.StatusCode,
		headers: resp.Header.Clone(),
		body:    body,
	}, nil
}

func materializeHTTPBody(body io.ReadCloser) (io.ReadCloser, error) {
	if body == nil {
		return http.NoBody, nil
	}
	tmp, err := os.CreateTemp("", "regimux-maven-proxy-upstream-*")
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
