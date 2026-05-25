package upstream

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/arcgolabs/clientx"
	clienthttp "github.com/arcgolabs/clientx/http"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"resty.dev/v3"
)

const defaultUserAgent = "regimux/dev"

type Client struct {
	upstreams *collectionmapping.OrderedMap[string, upstreamRuntime]
	logger    *slog.Logger
}

type upstreamRuntime struct {
	config Config
	client clienthttp.Client
	err    error
}

type requestOption func(*resty.Request)

func NewClient(configs map[string]Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	upstreams := collectionmapping.NewOrderedMapWithCapacity[string, upstreamRuntime](len(configs))
	for alias, cfg := range configs {
		cfg.Alias = alias
		runtime := upstreamRuntime{config: cfg}
		if cfgEnabled(cfg) {
			runtime.client, runtime.err = newHTTPClient(cfg)
			if runtime.err != nil {
				logger.Warn("create upstream http client failed", "alias", alias, "error", runtime.err)
			}
		}
		upstreams.Set(alias, runtime)
	}

	return &Client{
		upstreams: upstreams,
		logger:    logger,
	}
}

func (c *Client) Ping(ctx context.Context, alias string) error {
	runtime, err := c.upstream(alias)
	if err != nil {
		return err
	}
	requestURL := strings.TrimRight(runtime.config.Registry, "/") + "/v2/"
	resp, err := c.do(ctx, runtime, http.MethodGet, requestURL, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapStatus(resp.StatusCode, "ping")
	}
	return nil
}

func (c *Client) GetManifest(ctx context.Context, req GetManifestRequest) (*ManifestResponse, error) {
	runtime, err := c.upstream(req.UpstreamAlias)
	if err != nil {
		return nil, err
	}
	method := methodOr(req.Method, http.MethodGet)
	requestURL := registryURL(runtime.config.Registry, req.Repo, "manifests", req.Reference)
	var opts []requestOption
	if req.Accept != "" {
		opts = append(opts, withHeader("Accept", req.Accept))
	}
	resp, err := c.do(ctx, runtime, method, requestURL, repositoryScope(req.Repo, "pull"), opts...)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, mapStatus(resp.StatusCode, "manifest")
	}
	return &ManifestResponse{
		Body:      resp.Body,
		Digest:    resp.Header.Get("Docker-Content-Digest"),
		MediaType: contentType(resp.Header),
		Size:      contentLength(resp.Header),
		Headers:   resp.Header.Clone(),
	}, nil
}

func (c *Client) GetBlob(ctx context.Context, req GetBlobRequest) (*BlobResponse, error) {
	runtime, err := c.upstream(req.UpstreamAlias)
	if err != nil {
		return nil, err
	}
	method := methodOr(req.Method, http.MethodGet)
	requestURL := registryURL(runtime.config.Registry, req.Repo, "blobs", req.Digest)
	var opts []requestOption
	if req.Range != nil {
		opts = append(opts, withHeader("Range", req.Range.String()))
	}
	resp, err := c.do(ctx, runtime, method, requestURL, repositoryScope(req.Repo, "pull"), opts...)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, mapStatus(resp.StatusCode, "blob")
	}
	return &BlobResponse{
		Body:       resp.Body,
		Digest:     firstNonEmpty(resp.Header.Get("Docker-Content-Digest"), req.Digest),
		Size:       contentLength(resp.Header),
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
	}, nil
}

func (c *Client) ListTags(ctx context.Context, req ListTagsRequest) (*TagsResponse, error) {
	runtime, err := c.upstream(req.UpstreamAlias)
	if err != nil {
		return nil, err
	}
	requestURL := registryURL(runtime.config.Registry, req.Repo, "tags/list", "")
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	if req.N != "" {
		query.Set("n", req.N)
	}
	if req.Last != "" {
		query.Set("last", req.Last)
	}
	parsed.RawQuery = query.Encode()

	resp, err := c.do(ctx, runtime, http.MethodGet, parsed.String(), repositoryScope(req.Repo, "pull"))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, mapStatus(resp.StatusCode, "tags")
	}
	return &TagsResponse{Body: resp.Body, Headers: resp.Header.Clone()}, nil
}

func (c *Client) GetReferrers(ctx context.Context, req ReferrersRequest) (*ReferrersResponse, error) {
	runtime, err := c.upstream(req.UpstreamAlias)
	if err != nil {
		return nil, err
	}
	requestURL := registryURL(runtime.config.Registry, req.Repo, "referrers", req.Digest)
	resp, err := c.do(ctx, runtime, http.MethodGet, requestURL, repositoryScope(req.Repo, "pull"))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, mapStatus(resp.StatusCode, "referrers")
	}
	return &ReferrersResponse{
		Body:      resp.Body,
		MediaType: contentType(resp.Header),
		Headers:   resp.Header.Clone(),
	}, nil
}

func (c *Client) do(ctx context.Context, runtime upstreamRuntime, method, endpoint, scope string, opts ...requestOption) (*http.Response, error) {
	resp, err := c.execute(ctx, runtime, method, endpoint, opts...)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	challenge := parseBearerChallenge(resp.Header.Get("WWW-Authenticate"))
	if challenge.Realm == "" {
		return resp, nil
	}
	drainAndClose(resp.Body)

	token, err := c.fetchToken(ctx, runtime, challenge, scope)
	if err != nil {
		return nil, err
	}
	retryRuntime := runtime
	retryRuntime.config.Auth = AuthConfig{Type: "bearer", Token: token}
	return c.execute(ctx, retryRuntime, method, endpoint, opts...)
}

func (c *Client) execute(ctx context.Context, runtime upstreamRuntime, method, endpoint string, opts ...requestOption) (*http.Response, error) {
	if runtime.client == nil {
		return nil, fmt.Errorf("upstream http client is not configured")
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
		return nil, err
	}
	return rawHTTPResponse(resp)
}

func (c *Client) fetchToken(ctx context.Context, runtime upstreamRuntime, challenge bearerChallenge, fallbackScope string) (string, error) {
	realm, err := url.Parse(challenge.Realm)
	if err != nil {
		return "", err
	}
	query := realm.Query()
	if challenge.Service != "" {
		query.Set("service", challenge.Service)
	}
	if challenge.Scope != "" {
		query.Set("scope", challenge.Scope)
	} else if fallbackScope != "" {
		query.Set("scope", fallbackScope)
	}
	realm.RawQuery = query.Encode()

	req := runtime.client.R().SetDoNotParseResponse(true)
	if runtime.config.Auth.Username != "" || runtime.config.Auth.Password != "" {
		req.SetBasicAuth(runtime.config.Auth.Username, runtime.config.Auth.Password)
	}
	resp, err := runtime.client.Execute(ctx, req, http.MethodGet, realm.String())
	if err != nil {
		return "", err
	}
	raw, err := rawHTTPResponse(resp)
	if err != nil {
		return "", err
	}
	defer raw.Body.Close()
	if raw.StatusCode < 200 || raw.StatusCode >= 300 {
		return "", mapStatus(raw.StatusCode, "token")
	}
	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		IssuedAt    string `json:"issued_at"`
	}
	if err := decodeJSON(raw.Body, &tokenResp); err != nil {
		return "", err
	}
	token := firstNonEmpty(tokenResp.Token, tokenResp.AccessToken)
	if token == "" {
		return "", fmt.Errorf("upstream token response did not include a token")
	}
	return token, nil
}

func (c *Client) upstream(alias string) (upstreamRuntime, error) {
	if c == nil || c.upstreams == nil {
		return upstreamRuntime{}, fmt.Errorf("upstream registry is not configured")
	}
	runtime, ok := c.upstreams.Get(alias)
	if !ok || !cfgEnabled(runtime.config) {
		return upstreamRuntime{}, distribution.ErrNameUnknown.WithDetail("unknown upstream alias: " + alias)
	}
	if runtime.err != nil {
		return upstreamRuntime{}, distribution.ErrUpstream.WithDetail(runtime.err.Error())
	}
	return runtime, nil
}

func newHTTPClient(cfg Config) (clienthttp.Client, error) {
	httpClient, err := clienthttp.New(clienthttp.Config{
		BaseURL:   strings.TrimRight(cfg.Registry, "/"),
		Timeout:   cfg.HTTP.Timeout,
		UserAgent: defaultUserAgent,
		Retry: clientx.RetryConfig{
			Enabled:    cfg.HTTP.Retry.Enabled,
			MaxRetries: cfg.HTTP.Retry.MaxRetries,
			WaitMin:    cfg.HTTP.Retry.WaitMin,
			WaitMax:    cfg.HTTP.Retry.WaitMax,
		},
		TLS: clientx.TLSConfig{
			Enabled:            cfg.HTTP.TLS.Enabled,
			InsecureSkipVerify: cfg.HTTP.TLS.InsecureSkipVerify,
			ServerName:         cfg.HTTP.TLS.ServerName,
		},
	})
	if err != nil {
		return nil, err
	}
	if cfg.HTTP.Timeout == 0 {
		// Preserve RegiMux's previous no-client-timeout behavior. Per-request
		// cancellation still flows through context.
		httpClient.Raw().SetTimeout(0)
	}
	httpClient.Raw().Client().CheckRedirect = stripAuthOnCrossHostRedirect
	return httpClient, nil
}

func stripAuthOnCrossHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if req.URL.Host != via[0].URL.Host {
		req.Header.Del("Authorization")
	}
	if len(via) >= 5 {
		return http.ErrUseLastResponse
	}
	return nil
}

func prepareRequest(req *resty.Request, cfg Config) {
	req.SetHeader("User-Agent", defaultUserAgent)
	switch strings.ToLower(cfg.Auth.Type) {
	case "bearer":
		if cfg.Auth.Token != "" {
			req.SetHeader("Authorization", "Bearer "+cfg.Auth.Token)
		}
	case "basic", "dockerhub":
		if cfg.Auth.Username != "" || cfg.Auth.Password != "" {
			req.SetBasicAuth(cfg.Auth.Username, cfg.Auth.Password)
		}
	}
}

func withHeader(key, value string) requestOption {
	return func(req *resty.Request) {
		req.SetHeader(key, value)
	}
}

func rawHTTPResponse(resp *resty.Response) (*http.Response, error) {
	if resp == nil || resp.RawResponse == nil {
		return nil, fmt.Errorf("upstream response is empty")
	}
	if resp.Body != nil {
		resp.RawResponse.Body = resp.Body
	}
	return resp.RawResponse, nil
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func registryURL(registry, repo, operation, value string) string {
	base := strings.TrimRight(registry, "/")
	if value == "" {
		return base + "/v2/" + strings.Trim(repo, "/") + "/" + operation
	}
	return base + "/v2/" + strings.Trim(repo, "/") + "/" + operation + "/" + value
}

func repositoryScope(repo, action string) string {
	return "repository:" + repo + ":" + action
}

func mapStatus(status int, kind string) error {
	switch status {
	case http.StatusUnauthorized:
		return distribution.ErrUnauthorized
	case http.StatusForbidden:
		return distribution.ErrDenied
	case http.StatusNotFound:
		if kind == "blob" {
			return distribution.ErrBlobUnknown
		}
		return distribution.ErrManifestUnknown
	case http.StatusTooManyRequests:
		return distribution.ErrTooManyRequests
	default:
		if status >= 500 {
			return distribution.ErrUpstream.WithDetail(status)
		}
		return distribution.ErrUpstream.WithDetail(map[string]any{"status": status, "kind": kind})
	}
}

func methodOr(method, fallback string) string {
	if method == "" {
		return fallback
	}
	return method
}

func contentType(header http.Header) string {
	value := header.Get("Content-Type")
	if value == "" {
		return "application/octet-stream"
	}
	if before, _, ok := strings.Cut(value, ";"); ok {
		return strings.TrimSpace(before)
	}
	return value
}

func contentLength(header http.Header) int64 {
	value := header.Get("Content-Length")
	if value == "" {
		return -1
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cfgEnabled(cfg Config) bool {
	return strings.TrimSpace(cfg.Registry) != ""
}
