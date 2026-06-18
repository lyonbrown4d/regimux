package upstream

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func (c *Client) fetchToken(ctx context.Context, runtime upstreamRuntime, challenge bearerChallenge, fallbackScope string) (string, error) {
	tokenReq, err := newBearerTokenRequest(runtime.config, challenge, fallbackScope)
	if err != nil {
		return "", err
	}
	if token, ok := c.tokenCache.get(tokenReq.CacheKey); ok {
		return token, nil
	}

	token, err, _ := c.tokenGroup.Do(bearerTokenSingleflightKey(tokenReq.CacheKey), func() (string, error) {
		if token, ok := c.tokenCache.get(tokenReq.CacheKey); ok {
			return token, nil
		}
		rawResp, fetchErr := fetchTokenResponse(ctx, runtime, tokenReq.URL)
		if fetchErr != nil {
			return "", fetchErr
		}
		token, expiresAt, readErr := readTokenResponse(rawResp)
		if readErr != nil {
			return "", readErr
		}
		c.tokenCache.set(tokenReq.CacheKey, token, expiresAt)
		return token, nil
	})
	if err != nil {
		return "", wrapError(err, "fetch upstream bearer token")
	}
	return token, nil
}

func bearerTokenSingleflightKey(key bearerTokenCacheKey) string {
	return fmt.Sprintf("%q\x00%q\x00%q\x00%q\x00%q", key.Endpoint, key.Realm, key.Service, key.Scope, key.Username)
}

func fetchTokenResponse(ctx context.Context, runtime upstreamRuntime, endpoint string) (upstreamResponse, error) {
	req := runtime.client.R().SetResponseDoNotParse(true)
	if runtime.config.Auth.Username != "" || runtime.config.Auth.Password != "" {
		req.SetBasicAuth(runtime.config.Auth.Username, runtime.config.Auth.Password)
	}
	resp, err := runtime.client.Execute(ctx, req, http.MethodGet, endpoint)
	if err != nil {
		return upstreamResponse{}, wrapError(err, "fetch upstream bearer token")
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

	token := tokenResp.Token
	if token == "" {
		token = tokenResp.AccessToken
	}
	if token == "" {
		return "", time.Time{}, newError("upstream token response did not include a token")
	}
	return token, bearerTokenExpiresAt(tokenResp), nil
}
