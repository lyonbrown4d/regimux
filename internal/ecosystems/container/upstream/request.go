package upstream

import (
	"context"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
	retry "github.com/sethvargo/go-retry"
	"resty.dev/v3"
)

type requestOption func(*resty.Request)

type requestSpec struct {
	operation string
	method    string
	endpoint  string
	scope     string
	options   []requestOption
}

type requestAttempt struct {
	runtime upstreamRuntime
	spec    requestSpec
	state   requestAttemptState
	backoff retry.Backoff
	number  int
	total   int
}

func (c *Client) do(
	ctx context.Context,
	runtime upstreamRuntime,
	spec requestSpec,
) (upstreamResponse, error) {
	resp, err := c.execute(ctx, runtime, spec)
	if err != nil {
		return upstreamResponse{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return c.doBearerChallenge(ctx, runtime, spec, resp)
	}
	if resp.StatusCode == http.StatusForbidden {
		forbiddenEvidence, evidenceErr := c.publicMirrorAllowlistForbidden(ctx, runtime, runtime, spec, resp)
		if evidenceErr != nil {
			return upstreamResponse{}, evidenceErr
		}
		if forbiddenEvidence {
			return upstreamResponse{}, withAllowlistForbidden(spec.operation)
		}
	}
	return resp, nil
}

func (c *Client) doBearerChallenge(
	ctx context.Context,
	runtime upstreamRuntime,
	spec requestSpec,
	resp upstreamResponse,
) (upstreamResponse, error) {
	challenge := parseBearerChallenge(resp.Header.Get(distribution.HeaderWWWAuthenticate))
	if challenge.Realm == "" {
		return resp, nil
	}
	if drainErr := drainAndClose(resp.Body); drainErr != nil {
		return upstreamResponse{}, drainErr
	}

	token, err := c.fetchToken(ctx, runtime, challenge, spec.scope)
	if err != nil {
		return upstreamResponse{}, err
	}
	retryRuntime := runtime
	retryRuntime.config.Auth = AuthConfig{Type: distribution.AuthSchemeBearer, Token: token}
	retryResp, err := c.execute(ctx, retryRuntime, spec)
	if err != nil {
		return upstreamResponse{}, err
	}
	return c.handleBearerRetry(ctx, runtime, retryRuntime, spec, retryResp)
}

func (c *Client) handleBearerRetry(
	ctx context.Context,
	configuredRuntime upstreamRuntime,
	retryRuntime upstreamRuntime,
	spec requestSpec,
	retryResp upstreamResponse,
) (upstreamResponse, error) {
	if retryResp.StatusCode == http.StatusForbidden {
		forbiddenEvidence, evidenceErr := c.publicMirrorAllowlistForbidden(ctx, configuredRuntime, retryRuntime, spec, retryResp)
		if evidenceErr != nil {
			return upstreamResponse{}, evidenceErr
		}
		if forbiddenEvidence {
			return upstreamResponse{}, withAllowlistForbidden(spec.operation)
		}
	}
	if retryResp.StatusCode == http.StatusUnauthorized || retryResp.StatusCode == http.StatusForbidden {
		return upstreamResponse{}, closeBodyWithError(
			retryResp.Body,
			withAuthenticatedUpstreamStatus(retryResp.StatusCode, spec.operation),
		)
	}
	return retryResp, nil
}

func (c *Client) publicMirrorAllowlistForbidden(
	ctx context.Context,
	configuredRuntime upstreamRuntime,
	requestRuntime upstreamRuntime,
	spec requestSpec,
	resp upstreamResponse,
) (bool, error) {
	if resp.StatusCode != http.StatusForbidden || !isPublicAnonymousMirror(configuredRuntime) {
		return false, nil
	}
	return c.confirmAllowlistForbidden(ctx, requestRuntime, spec, resp)
}

func (c *Client) execute(
	ctx context.Context,
	runtime upstreamRuntime,
	spec requestSpec,
) (upstreamResponse, error) {
	if runtime.client == nil {
		return upstreamResponse{}, newError("upstream http client is not configured")
	}
	maxAttempts := maxUpstreamAttempts(runtime.config.HTTP.Retry)
	state := requestAttemptState{
		startedAt: time.Now(),
		size:      -1,
	}
	backoff := upstreamRetryBackoff(runtime.config.HTTP.Retry)

	for number := range maxAttempts {
		attempt := requestAttempt{
			runtime: runtime,
			spec:    spec,
			state:   state,
			backoff: backoff,
			number:  number + 1,
			total:   maxAttempts,
		}
		resp, shouldRetry, err := c.executeAttempt(ctx, attempt)
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
	status    int
	size      int64
}

func (c *Client) executeAttempt(
	ctx context.Context,
	attempt requestAttempt,
) (upstreamResponse, bool, error) {
	resp, err := c.executeOnce(ctx, attempt)
	if err != nil {
		return c.handleAttemptError(ctx, attempt, err)
	}

	attempt.state.status = resp.StatusCode
	attempt.state.size = contentLength(resp.Header)
	if !shouldRetryUpstreamStatus(resp.StatusCode) || attempt.number >= attempt.total {
		c.publishAttempt(ctx, attempt, nil)
		return resp, false, nil
	}
	if err := c.prepareRetry(ctx, attempt, &resp, nil); err != nil {
		c.publishAttempt(ctx, attempt, err)
		return upstreamResponse{}, false, err
	}
	return upstreamResponse{}, true, nil
}

func (c *Client) handleAttemptError(
	ctx context.Context,
	attempt requestAttempt,
	err error,
) (upstreamResponse, bool, error) {
	if !shouldRetryUpstreamError(err) || attempt.number >= attempt.total {
		c.publishAttempt(ctx, attempt, err)
		return upstreamResponse{}, false, err
	}
	if retryErr := c.prepareRetry(ctx, attempt, nil, err); retryErr != nil {
		c.publishAttempt(ctx, attempt, retryErr)
		return upstreamResponse{}, false, retryErr
	}
	return upstreamResponse{}, true, nil
}

func (c *Client) prepareRetry(
	ctx context.Context,
	attempt requestAttempt,
	resp *upstreamResponse,
	cause error,
) error {
	if resp != nil {
		if err := drainAndClose(resp.Body); err != nil {
			return err
		}
	}
	wait, stop := attempt.backoff.Next()
	if stop {
		return nil
	}
	c.logUpstreamRetry(ctx, attempt, cause, wait)
	return waitRetry(ctx, wait)
}

func (c *Client) publishAttempt(
	ctx context.Context,
	attempt requestAttempt,
	err error,
) {
	c.publishUpstreamRequest(
		ctx,
		attempt,
		time.Since(attempt.state.startedAt),
		err,
	)
}

func (c *Client) executeOnce(
	ctx context.Context,
	attempt requestAttempt,
) (upstreamResponse, error) {
	req := attempt.runtime.client.R().SetResponseDoNotParse(true)
	prepareRequest(req, attempt.runtime.config)
	for _, option := range attempt.spec.options {
		if option != nil {
			option(req)
		}
	}
	resp, err := attempt.runtime.client.Execute(
		ctx,
		req,
		attempt.spec.method,
		attempt.spec.endpoint,
	)
	if err != nil {
		return upstreamResponse{}, wrapError(
			err,
			"execute upstream request %s %s",
			attempt.spec.method,
			attempt.spec.endpoint,
		)
	}
	return rawUpstreamResponse(resp)
}
