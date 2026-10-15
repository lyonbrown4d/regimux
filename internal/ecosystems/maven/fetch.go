package maven

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	clienthttp "github.com/arcgolabs/clientx/http"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/spool"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

const defaultMavenAttemptTimeout = 10 * time.Second

type mavenClientKey struct {
	factory            *clientfactory.Factory
	baseURL            string
	timeout            time.Duration
	http2              bool
	retryEnabled       bool
	maxRetries         int
	retryWaitMin       time.Duration
	retryWaitMax       time.Duration
	tlsEnabled         bool
	insecureSkipVerify bool
	serverName         string
}

var mavenClients = struct {
	sync.Mutex
	clients map[mavenClientKey]clienthttp.Client
}{
	clients: make(map[mavenClientKey]clienthttp.Client),
}

func (s *Service) fetchPhysical(ctx context.Context, cfg config.UpstreamConfig, upstreamAlias string, requestRoute Route, method string) (*upstreamFetch, error) {
	endpoints := ecosystem.UpstreamEndpoints(ctx, s.metadata, ecosystem.Maven, upstreamAlias, cfg)
	var lastErr error
	var lastRetryable *upstreamFetch
	for _, endpoint := range endpoints {
		requestURL := strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(requestRoute.UpstreamTail, "/")
		requestURL = urlWithQuery(requestURL, requestRoute.Query)
		attemptCtx, cancel := mavenAttemptContext(ctx, cfg.HTTP.AttemptTimeout)
		resp, err := s.fetchURL(attemptCtx, cfg, requestURL, method)
		cancel()
		if err == nil {
			if retryableMavenStatus(resp.status) {
				if lastRetryable != nil {
					closeReadCloser(lastRetryable.body, s.logger, "close superseded maven retryable response body")
				}
				lastRetryable = resp
				continue
			}
			if lastRetryable != nil {
				closeReadCloser(lastRetryable.body, s.logger, "close superseded maven retryable response body")
			}
			return resp, nil
		}
		if ctx.Err() != nil {
			if lastRetryable != nil {
				closeReadCloser(lastRetryable.body, s.logger, "close canceled maven retryable response body")
			}
			return nil, wrapError(ctx.Err(), "fetch maven upstream")
		}
		lastErr = err
	}
	if lastRetryable != nil {
		return lastRetryable, nil
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
	if factory == nil {
		factory = clientfactory.New(s.logger)
	}
	key := mavenClientKey{
		factory:            factory,
		baseURL:            clientfactory.NormalizeBaseURL(baseURL),
		timeout:            cfg.HTTP.Timeout,
		http2:              cfg.HTTP.HTTP2.Enabled,
		retryEnabled:       cfg.HTTP.Retry.Enabled,
		maxRetries:         cfg.HTTP.Retry.MaxRetries,
		retryWaitMin:       cfg.HTTP.Retry.WaitMin,
		retryWaitMax:       cfg.HTTP.Retry.WaitMax,
		tlsEnabled:         cfg.HTTP.TLS.Enabled,
		insecureSkipVerify: cfg.HTTP.TLS.InsecureSkipVerify,
		serverName:         cfg.HTTP.TLS.ServerName,
	}
	mavenClients.Lock()
	defer mavenClients.Unlock()
	if client, ok := mavenClients.clients[key]; ok {
		return client, nil
	}
	client, err := upstreamhttp.NewClient(factory, cfg, baseURL, "maven.clientx")
	if err != nil {
		return nil, wrapError(err, "create maven upstream client")
	}
	mavenClients.clients[key] = client
	return client, nil
}

func mavenAttemptContext(ctx context.Context, configured time.Duration) (context.Context, context.CancelFunc) {
	if configured <= 0 {
		configured = defaultMavenAttemptTimeout
	}
	return context.WithTimeout(ctx, configured)
}

func retryableMavenStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	default:
		return status >= http.StatusInternalServerError && status <= 599
	}
}

func materializeHTTPBody(body io.ReadCloser) (io.ReadCloser, error) {
	materialized, err := spool.MaterializeReadCloser(body, "regimux-maven-upstream-*")
	if err != nil {
		return nil, wrapError(err, "materialize maven upstream body")
	}
	return materialized, nil
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
