package upstream

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	retry "github.com/sethvargo/go-retry"
	"resty.dev/v3"
)

type requestOption func(*resty.Request)

func (c *Client) do(ctx context.Context, runtime upstreamRuntime, operation, method, endpoint, scope string, opts ...requestOption) (upstreamResponse, error) {
	resp, err := c.execute(ctx, runtime, operation, method, endpoint, opts...)
	if err != nil {
		return upstreamResponse{}, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	challenge := parseBearerChallenge(resp.Header.Get(distribution.HeaderWWWAuthenticate))
	if challenge.Realm == "" {
		return resp, nil
	}
	if drainErr := drainAndClose(resp.Body); drainErr != nil {
		return upstreamResponse{}, drainErr
	}

	token, err := c.fetchToken(ctx, runtime, challenge, scope)
	if err != nil {
		return upstreamResponse{}, err
	}
	retryRuntime := runtime
	retryRuntime.config.Auth = AuthConfig{Type: distribution.AuthSchemeBearer, Token: token}
	return c.execute(ctx, retryRuntime, operation, method, endpoint, opts...)
}

func (c *Client) execute(ctx context.Context, runtime upstreamRuntime, operation, method, endpoint string, opts ...requestOption) (upstreamResponse, error) {
	if runtime.client == nil {
		return upstreamResponse{}, newError("upstream http client is not configured")
	}
	maxAttempts := maxUpstreamAttempts(runtime.config.HTTP.Retry)
	state := requestAttemptState{
		startedAt: time.Now(),
		operation: operation,
		method:    method,
		endpoint:  endpoint,
		size:      -1,
	}
	backoff := upstreamRetryBackoff(runtime.config.HTTP.Retry)

	for attempt := range maxAttempts {
		resp, shouldRetry, err := c.executeAttempt(ctx, runtime, state, backoff, attempt+1, maxAttempts, opts...)
		if err != nil {
			return upstreamResponse{}, err
		}
		if shouldRetry {
			continue
		}
		return resp, nil
	}

	return upstreamResponse{}, newError("upstream request did not execute")
}

type requestAttemptState struct {
	startedAt time.Time
	operation string
	method    string
	endpoint  string
	status    int
	size      int64
}

func (c *Client) executeAttempt(
	ctx context.Context,
	runtime upstreamRuntime,
	state requestAttemptState,
	backoff retry.Backoff,
	attempt int,
	maxAttempts int,
	opts ...requestOption,
) (upstreamResponse, bool, error) {
	resp, err := c.executeOnce(ctx, runtime, state.method, state.endpoint, opts...)
	if err != nil {
		c.publishAttempt(ctx, runtime, state, attempt, err)
		return upstreamResponse{}, false, err
	}

	state.status = resp.StatusCode
	state.size = contentLength(resp.Header)
	if !shouldRetryUpstreamStatus(resp.StatusCode) || attempt >= maxAttempts {
		c.publishAttempt(ctx, runtime, state, attempt, nil)
		return resp, false, nil
	}
	if err := c.prepareRetry(ctx, runtime, state, backoff, resp, attempt, maxAttempts); err != nil {
		c.publishAttempt(ctx, runtime, state, attempt, err)
		return upstreamResponse{}, false, err
	}
	return upstreamResponse{}, true, nil
}

func (c *Client) prepareRetry(
	ctx context.Context,
	runtime upstreamRuntime,
	state requestAttemptState,
	backoff retry.Backoff,
	resp upstreamResponse,
	attempt int,
	maxAttempts int,
) error {
	if err := drainAndClose(resp.Body); err != nil {
		return err
	}
	wait, stop := backoff.Next()
	if stop {
		return nil
	}
	c.logUpstreamRetry(ctx, runtime, state.operation, state.method, state.endpoint, resp.StatusCode, attempt, maxAttempts, wait)
	return waitRetry(ctx, wait)
}

func (c *Client) publishAttempt(ctx context.Context, runtime upstreamRuntime, state requestAttemptState, attempts int, err error) {
	c.publishUpstreamRequest(ctx, runtime, state, attempts, time.Since(state.startedAt), err)
}

func (c *Client) executeOnce(ctx context.Context, runtime upstreamRuntime, method, endpoint string, opts ...requestOption) (upstreamResponse, error) {
	req := runtime.client.R().SetDoNotParseResponse(true)
	prepareRequest(req, runtime.config)
	for _, opt := range opts {
		if opt != nil {
			opt(req)
		}
	}
	resp, err := runtime.client.Execute(ctx, req, method, endpoint)
	if err != nil {
		return upstreamResponse{}, wrapError(err, "execute upstream request %s %s", method, endpoint)
	}
	return rawUpstreamResponse(resp)
}

func maxUpstreamAttempts(cfg HTTPRetryConfig) int {
	if !cfg.Enabled {
		return 1
	}
	return max(1, cfg.MaxRetries+1)
}

func shouldRetryUpstreamStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func upstreamRetryBackoff(cfg HTTPRetryConfig) retry.Backoff {
	wait := cfg.WaitMin
	if wait <= 0 {
		wait = 100 * time.Millisecond
	}
	backoff := retry.NewExponential(wait)
	if cfg.WaitMax > 0 {
		backoff = retry.WithCappedDuration(cfg.WaitMax, backoff)
	}
	maxRetries := max(0, cfg.MaxRetries)
	return retry.WithMaxRetries(uint64(maxRetries), backoff)
}

func waitRetry(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return wrapError(ctx.Err(), "wait upstream retry")
	}
}

func (c *Client) publishUpstreamRequest(ctx context.Context, runtime upstreamRuntime, state requestAttemptState, attempts int, duration time.Duration, err error) {
	if c == nil || c.events == nil {
		return
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	if publishErr := events.Publish(ctx, c.events, events.UpstreamRequest{
		Alias:     runtime.config.Alias,
		Operation: state.operation,
		Registry:  runtime.config.Registry,
		Method:    strings.ToUpper(state.method),
		Path:      requestPath(state.endpoint),
		Status:    state.status,
		Attempts:  attempts,
		Duration:  duration,
		Size:      state.size,
		Error:     message,
	}); publishErr != nil && c.logger != nil {
		c.logger.DebugContext(ctx, "publish upstream request event failed", "error", publishErr)
	}
}

func (c *Client) logUpstreamRetry(ctx context.Context, runtime upstreamRuntime, operation, method, endpoint string, status, attempt, maxAttempts int, wait time.Duration) {
	if c == nil || c.logger == nil {
		return
	}
	c.logger.WarnContext(ctx,
		"retrying upstream request",
		"alias", runtime.config.Alias,
		"operation", operation,
		"method", method,
		"registry", runtime.config.Registry,
		"path", requestPath(endpoint),
		"status", status,
		"attempt", attempt,
		"max_attempts", maxAttempts,
		"wait", wait,
	)
}

func requestPath(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || parsed.Path == "" {
		return endpoint
	}
	if parsed.RawQuery == "" {
		return parsed.Path
	}
	return parsed.Path + "?" + parsed.RawQuery
}
