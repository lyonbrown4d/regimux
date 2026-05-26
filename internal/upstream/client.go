package upstream

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/samber/lo"
)

type Client struct {
	upstreams  *collectionmapping.OrderedMap[string, *upstreamPool]
	tokenCache *bearerTokenCache
	logger     *slog.Logger
}

func NewClient(configs *collectionmapping.OrderedMap[string, Config], logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	upstreams := collectionmapping.NewOrderedMap[string, *upstreamPool]()
	if configs == nil {
		return &Client{
			upstreams:  upstreams,
			tokenCache: newBearerTokenCache(),
			logger:     logger,
		}
	}
	upstreams = collectionmapping.NewOrderedMapWithCapacity[string, *upstreamPool](configs.Len())
	configs.Range(func(alias string, cfg Config) bool {
		cfg.Alias = alias
		upstreams.Set(alias, newUpstreamPool(cfg, logger))
		return true
	})

	return &Client{
		upstreams:  upstreams,
		tokenCache: newBearerTokenCache(),
		logger:     logger,
	}
}

func (c *Client) Ping(ctx context.Context, alias string) error {
	return c.doWithFailover(ctx, alias, "ping", func(runtime upstreamRuntime) error {
		requestURL := strings.TrimRight(runtime.config.Registry, "/") + "/v2/"
		resp, err := c.do(ctx, runtime, http.MethodGet, requestURL, "")
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, "ping"))
		}
		return closeBody(resp.Body)
	})
}

func (c *Client) GetManifest(ctx context.Context, req GetManifestRequest) (*ManifestResponse, error) {
	var out *ManifestResponse
	err := c.doWithFailover(ctx, req.UpstreamAlias, "manifest", func(runtime upstreamRuntime) error {
		method := methodOr(req.Method, http.MethodGet)
		requestURL := registryURL(runtime.config.Registry, req.Repo, "manifests", req.Reference)
		var opts []requestOption
		if req.Accept != "" {
			opts = append(opts, withHeader("Accept", req.Accept))
		}
		resp, err := c.do(ctx, runtime, method, requestURL, pullRepositoryScope(req.Repo), opts...)
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, "manifest"))
		}
		out = &ManifestResponse{
			Body:      resp.Body,
			Digest:    resp.Header.Get("Docker-Content-Digest"),
			MediaType: contentType(resp.Header),
			Size:      contentLength(resp.Header),
			Headers:   resp.Header.Clone(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetBlob(ctx context.Context, req GetBlobRequest) (*BlobResponse, error) {
	var out *BlobResponse
	err := c.doWithFailover(ctx, req.UpstreamAlias, "blob", func(runtime upstreamRuntime) error {
		method := methodOr(req.Method, http.MethodGet)
		requestURL := registryURL(runtime.config.Registry, req.Repo, "blobs", req.Digest)
		var opts []requestOption
		if req.Range != nil {
			opts = append(opts, withHeader("Range", req.Range.String()))
		}
		resp, err := c.do(ctx, runtime, method, requestURL, pullRepositoryScope(req.Repo), opts...)
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, "blob"))
		}
		out = &BlobResponse{
			Body:       resp.Body,
			Digest:     lo.CoalesceOrEmpty(resp.Header.Get("Docker-Content-Digest"), req.Digest),
			Size:       contentLength(resp.Header),
			StatusCode: resp.StatusCode,
			Headers:    resp.Header.Clone(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListTags(ctx context.Context, req ListTagsRequest) (*TagsResponse, error) {
	var out *TagsResponse
	err := c.doWithFailover(ctx, req.UpstreamAlias, "tags", func(runtime upstreamRuntime) error {
		requestURL, err := tagsURL(runtime.config.Registry, req)
		if err != nil {
			return err
		}

		resp, err := c.do(ctx, runtime, http.MethodGet, requestURL, pullRepositoryScope(req.Repo))
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, "tags"))
		}
		out = &TagsResponse{Body: resp.Body, Headers: resp.Header.Clone()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetReferrers(ctx context.Context, req ReferrersRequest) (*ReferrersResponse, error) {
	var out *ReferrersResponse
	err := c.doWithFailover(ctx, req.UpstreamAlias, "referrers", func(runtime upstreamRuntime) error {
		requestURL := registryURL(runtime.config.Registry, req.Repo, "referrers", req.Digest)
		resp, err := c.do(ctx, runtime, http.MethodGet, requestURL, pullRepositoryScope(req.Repo))
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, "referrers"))
		}
		out = &ReferrersResponse{
			Body:      resp.Body,
			MediaType: contentType(resp.Header),
			Headers:   resp.Header.Clone(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
