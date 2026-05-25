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
	"time"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

const defaultUserAgent = "regimux/dev"

type Client struct {
	upstreams *collectionmapping.OrderedMap[string, Config]
	client    *http.Client
	logger    *slog.Logger
}

func NewClient(configs map[string]Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	upstreams := collectionmapping.NewOrderedMapWithCapacity[string, Config](len(configs))
	for alias, cfg := range configs {
		cfg.Alias = alias
		upstreams.Set(alias, cfg)
	}

	return &Client{
		upstreams: upstreams,
		client: &http.Client{
			Timeout: 0,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
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
			},
		},
		logger: logger,
	}
}

func (c *Client) Ping(ctx context.Context, alias string) error {
	cfg, err := c.upstream(alias)
	if err != nil {
		return err
	}
	requestURL := strings.TrimRight(cfg.Registry, "/") + "/v2/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req, cfg, "")
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
	cfg, err := c.upstream(req.UpstreamAlias)
	if err != nil {
		return nil, err
	}
	method := methodOr(req.Method, http.MethodGet)
	requestURL := registryURL(cfg.Registry, req.Repo, "manifests", req.Reference)
	httpReq, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	if req.Accept != "" {
		httpReq.Header.Set("Accept", req.Accept)
	}
	resp, err := c.do(httpReq, cfg, repositoryScope(req.Repo, "pull"))
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
	cfg, err := c.upstream(req.UpstreamAlias)
	if err != nil {
		return nil, err
	}
	method := methodOr(req.Method, http.MethodGet)
	requestURL := registryURL(cfg.Registry, req.Repo, "blobs", req.Digest)
	httpReq, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	if req.Range != nil {
		httpReq.Header.Set("Range", req.Range.String())
	}
	resp, err := c.do(httpReq, cfg, repositoryScope(req.Repo, "pull"))
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
	cfg, err := c.upstream(req.UpstreamAlias)
	if err != nil {
		return nil, err
	}
	requestURL := registryURL(cfg.Registry, req.Repo, "tags/list", "")
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(httpReq, cfg, repositoryScope(req.Repo, "pull"))
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
	cfg, err := c.upstream(req.UpstreamAlias)
	if err != nil {
		return nil, err
	}
	requestURL := registryURL(cfg.Registry, req.Repo, "referrers", req.Digest)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(httpReq, cfg, repositoryScope(req.Repo, "pull"))
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

func (c *Client) do(req *http.Request, cfg Config, scope string) (*http.Response, error) {
	prepareRequest(req, cfg)
	resp, err := c.client.Do(req)
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
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	token, err := c.fetchToken(req.Context(), challenge, cfg, scope)
	if err != nil {
		return nil, err
	}
	retry := req.Clone(req.Context())
	retry.Header = req.Header.Clone()
	retry.Header.Set("Authorization", "Bearer "+token)
	return c.client.Do(retry)
}

func (c *Client) fetchToken(ctx context.Context, challenge bearerChallenge, cfg Config, fallbackScope string) (string, error) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", err
	}
	if cfg.Auth.Username != "" || cfg.Auth.Password != "" {
		req.SetBasicAuth(cfg.Auth.Username, cfg.Auth.Password)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", mapStatus(resp.StatusCode, "token")
	}
	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		IssuedAt    string `json:"issued_at"`
	}
	if err := decodeJSON(resp.Body, &tokenResp); err != nil {
		return "", err
	}
	token := firstNonEmpty(tokenResp.Token, tokenResp.AccessToken)
	if token == "" {
		return "", fmt.Errorf("upstream token response did not include a token")
	}
	return token, nil
}

func (c *Client) upstream(alias string) (Config, error) {
	if c == nil || c.upstreams == nil {
		return Config{}, fmt.Errorf("upstream registry is not configured")
	}
	cfg, ok := c.upstreams.Get(alias)
	if !ok || !cfgEnabled(cfg) {
		return Config{}, distribution.ErrNameUnknown.WithDetail("unknown upstream alias: " + alias)
	}
	return cfg, nil
}

func prepareRequest(req *http.Request, cfg Config) {
	req.Header.Set("User-Agent", defaultUserAgent)
	switch strings.ToLower(cfg.Auth.Type) {
	case "bearer":
		if cfg.Auth.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Auth.Token)
		}
	case "basic", "dockerhub":
		if cfg.Auth.Username != "" || cfg.Auth.Password != "" {
			req.SetBasicAuth(cfg.Auth.Username, cfg.Auth.Password)
		}
	}
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
	_ = time.Second
	return strings.TrimSpace(cfg.Registry) != ""
}
