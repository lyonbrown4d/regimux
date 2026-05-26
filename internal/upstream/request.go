package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"resty.dev/v3"
)

type requestOption func(*resty.Request)

func (c *Client) do(ctx context.Context, runtime upstreamRuntime, method, endpoint, scope string, opts ...requestOption) (upstreamResponse, error) {
	resp, err := c.execute(ctx, runtime, method, endpoint, opts...)
	if err != nil {
		return upstreamResponse{}, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	challenge := parseBearerChallenge(resp.Header.Get("WWW-Authenticate"))
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
	retryRuntime.config.Auth = AuthConfig{Type: "bearer", Token: token}
	return c.execute(ctx, retryRuntime, method, endpoint, opts...)
}

func (c *Client) execute(ctx context.Context, runtime upstreamRuntime, method, endpoint string, opts ...requestOption) (upstreamResponse, error) {
	if runtime.client == nil {
		return upstreamResponse{}, errors.New("upstream http client is not configured")
	}
	req := runtime.client.R().SetDoNotParseResponse(true)
	prepareRequest(req, runtime.config)
	for _, opt := range opts {
		if opt != nil {
			opt(req)
		}
	}
	resp, err := runtime.client.Execute(ctx, req, method, endpoint)
	if err != nil {
		return upstreamResponse{}, fmt.Errorf("execute upstream request %s %s: %w", method, endpoint, err)
	}
	return rawUpstreamResponse(resp)
}

func (c *Client) fetchToken(ctx context.Context, runtime upstreamRuntime, challenge bearerChallenge, fallbackScope string) (string, error) {
	tokenReq, err := newBearerTokenRequest(runtime.config, challenge, fallbackScope)
	if err != nil {
		return "", err
	}
	if token, ok := c.tokenCache.get(tokenReq.CacheKey); ok {
		return token, nil
	}

	raw, err := fetchTokenResponse(ctx, runtime, tokenReq.URL)
	if err != nil {
		return "", err
	}
	token, expiresAt, err := readTokenResponse(raw)
	if err != nil {
		return "", err
	}
	c.tokenCache.set(tokenReq.CacheKey, token, expiresAt)
	return token, nil
}

func fetchTokenResponse(ctx context.Context, runtime upstreamRuntime, endpoint string) (upstreamResponse, error) {
	req := runtime.client.R().SetDoNotParseResponse(true)
	if runtime.config.Auth.Username != "" || runtime.config.Auth.Password != "" {
		req.SetBasicAuth(runtime.config.Auth.Username, runtime.config.Auth.Password)
	}
	resp, err := runtime.client.Execute(ctx, req, http.MethodGet, endpoint)
	if err != nil {
		return upstreamResponse{}, fmt.Errorf("fetch upstream bearer token: %w", err)
	}
	return rawUpstreamResponse(resp)
}

func readTokenResponse(raw upstreamResponse) (string, time.Time, error) {
	if raw.StatusCode < 200 || raw.StatusCode >= 300 {
		return "", time.Time{}, closeBodyWithError(raw.Body, mapStatus(raw.StatusCode, "token"))
	}

	var tokenResp bearerTokenResponse
	if err := decodeJSON(raw.Body, &tokenResp); err != nil {
		return "", time.Time{}, closeBodyWithError(raw.Body, err)
	}
	if err := closeBody(raw.Body); err != nil {
		return "", time.Time{}, err
	}

	token := firstNonEmpty(tokenResp.Token, tokenResp.AccessToken)
	if token == "" {
		return "", time.Time{}, errors.New("upstream token response did not include a token")
	}
	return token, bearerTokenExpiresAt(tokenResp), nil
}
