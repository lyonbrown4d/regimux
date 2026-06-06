package golang

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
	req, err := http.NewRequestWithContext(ctx, methodOr(method, http.MethodGet), requestURL, http.NoBody)
	if err != nil {
		return nil, wrapError(err, "create go proxy upstream request")
	}
	req.Header.Set("User-Agent", "regimux/dev")
	applyAuth(req, cfg.Auth)

	client := s.client
	if cfg.HTTP.Timeout > 0 {
		client = &http.Client{Timeout: cfg.HTTP.Timeout}
	}
	resp, err := client.Do(req)
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
		status:  resp.StatusCode,
		headers: resp.Header.Clone(),
		body:    body,
	}, nil
}

func materializeHTTPBody(body io.ReadCloser) (io.ReadCloser, error) {
	if body == nil {
		return http.NoBody, nil
	}
	tmp, err := os.CreateTemp("", "regimux-go-proxy-upstream-*")
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
