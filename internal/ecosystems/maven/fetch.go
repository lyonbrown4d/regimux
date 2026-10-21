package maven

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	clienthttp "github.com/arcgolabs/clientx/http"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/spool"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

const defaultMavenAttemptTimeout = 10 * time.Second
const defaultMavenBodyIdleTimeout = 30 * time.Second

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

//nolint:cyclop,gocognit,gocyclo // Endpoint failover is one stateful attempt loop.
func (s *Service) fetchPhysical(ctx context.Context, cfg config.UpstreamConfig, upstreamAlias string, requestRoute Route, method string) (*upstreamFetch, error) {
	endpoints := ecosystem.UpstreamEndpoints(ctx, s.metadata, ecosystem.Maven, upstreamAlias, cfg)
	state := mavenEndpointResult{}
	defer state.close(s.logger)
	for index, endpoint := range endpoints {
		requestURL := strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(requestRoute.UpstreamTail, "/")
		requestURL = urlWithQuery(requestURL, requestRoute.Query)
		started := time.Now()
		attemptTimeout := cfg.HTTP.AttemptTimeout
		if attemptTimeout <= 0 {
			attemptTimeout = defaultMavenAttemptTimeout
		}
		attemptCtx, cancel := context.WithCancel(ctx)
		attemptTimer := time.AfterFunc(attemptTimeout, cancel)
		resp, err := s.fetchURL(attemptCtx, cfg, requestURL, method, cancel, attemptTimer.Stop)
		status := 0
		if resp != nil {
			status = resp.status
		}
		eventErr := err
		if ctx.Err() != nil {
			eventErr = ctx.Err()
		}
		s.publishUpstreamRequest(ctx, requestRoute, endpoint, methodOrGet(method), status, time.Since(started), eventErr)
		canFailOver := status == http.StatusNotFound || retryableMavenStatus(status)
		if err != nil || canFailOver {
			s.publishUpstreamFailover(ctx, requestRoute, endpoint, methodOrGet(method), status, eventErr, index+1 < len(endpoints))
		}
		if ctx.Err() != nil {
			if resp != nil {
				closeReadCloser(resp.body, s.logger, "close canceled maven endpoint response body")
			}
			return nil, wrapError(ctx.Err(), "fetch maven upstream")
		}
		if state.remember(resp, err, s.logger) {
			continue
		}
		return resp, nil
	}
	return state.take(cfg, endpoints)
}

func (s *Service) fetchURL(ctx context.Context, cfg config.UpstreamConfig, requestURL, method string, cancel context.CancelFunc, stopAttemptTimeout func() bool) (*upstreamFetch, error) {
	headers := http.Header{}
	headers.Set("User-Agent", defaultUserAgent)
	resp, err := s.doFetch(ctx, cfg, requestURL, upstreamhttp.Request{
		Method:  methodOrGet(method),
		URL:     requestURL,
		Headers: headers,
		Auth:    cfg.Auth,
	})
	if err != nil {
		stopAttemptTimeout()
		cancel()
		return nil, wrapError(err, "send maven upstream request")
	}
	stopAttemptTimeout()
	resp.Body = upstreamhttp.WithIdleTimeout(resp.Body, cancel, defaultMavenBodyIdleTimeout)
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
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if s.closed {
		return nil, errors.New("maven service is closed")
	}
	factory := s.factory
	if factory == nil {
		factory = clientfactory.New(s.logger)
		s.factory = factory
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
	if s.clients == nil {
		s.clients = make(map[mavenClientKey]clienthttp.Client)
	}
	if client, ok := s.clients[key]; ok {
		return client, nil
	}
	client, err := upstreamhttp.NewClient(factory, cfg, baseURL, "maven.clientx")
	if err != nil {
		return nil, wrapError(err, "create maven upstream client")
	}
	s.clients[key] = client
	return client, nil
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.clientMu.Lock()
		s.closed = true
		pool := s.fillPool
		clients := s.clients
		s.clients = nil
		s.clientMu.Unlock()
		if pool != nil {
			s.closeErr = pool.close()
		}
		for _, client := range clients {
			if client != nil {
				s.closeErr = errors.Join(s.closeErr, client.Close())
			}
		}
	})
	return s.closeErr
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
