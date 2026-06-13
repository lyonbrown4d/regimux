package dist

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
	req, err := http.NewRequestWithContext(ctx, methodOrGet(distReq.Method), requestURL, http.NoBody)
	if err != nil {
		return nil, wrapError(err, "create dist upstream request")
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	if rangeHeader := strings.TrimSpace(distReq.Range); rangeHeader != "" && methodOrGet(distReq.Method) == http.MethodGet {
		req.Header.Set("Range", rangeHeader)
	}
	applyAuth(req, cfg.Auth)

	client, err := s.clientFor(cfg, requestURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
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
		status:  resp.StatusCode,
		headers: resp.Header.Clone(),
		body:    body,
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
	client, err := factory.RawUpstreamHTTP(cfg, baseURL, "dist.clientx")
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
