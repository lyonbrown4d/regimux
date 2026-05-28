package upstream

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/lo"
	"golang.org/x/sync/singleflight"
)

type Client struct {
	upstreams  *collectionmapping.OrderedMap[string, *upstreamPool]
	tokenCache *bearerTokenCache
	tokenGroup singleflight.Group
	workers    *worker.Pools
	events     events.Bus
	logger     *slog.Logger
}

const (
	operationPing      = "ping"
	operationManifest  = "manifest"
	operationBlob      = "blob"
	operationTags      = "tags"
	operationReferrers = "referrers"

	endpointManifest  = "manifests"
	endpointBlob      = "blobs"
	endpointReferrers = "referrers"
)

type ClientDependencies struct {
	Configs *collectionmapping.OrderedMap[string, Config]
	Logger  *slog.Logger
	Pools   *worker.Pools
	Bus     events.Bus
}

func NewClient(deps ClientDependencies) *Client {
	configs := deps.Configs
	logger := deps.Logger
	pools := deps.Pools
	bus := deps.Bus
	if logger == nil {
		logger = slog.Default()
	}
	upstreams := collectionmapping.NewOrderedMap[string, *upstreamPool]()
	if configs == nil {
		return &Client{
			upstreams:  upstreams,
			tokenCache: newBearerTokenCache(),
			workers:    pools,
			events:     bus,
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
		workers:    pools,
		events:     bus,
		logger:     logger,
	}
}

func NewClientFromConfigs(
	configs *collectionmapping.OrderedMap[string, Config],
	logger *slog.Logger,
	pools *worker.Pools,
	bus events.Bus,
) *Client {
	return NewClient(ClientDependencies{
		Configs: configs,
		Logger:  logger,
		Pools:   pools,
		Bus:     bus,
	})
}

func (c *Client) Ping(ctx context.Context, alias string) error {
	release, err := c.doWithFailover(ctx, failoverRequest{alias: alias, operation: operationPing}, func(runtime upstreamRuntime) error {
		requestURL := strings.TrimRight(runtime.config.Registry, "/") + registryAPIVersionPath
		resp, err := c.do(ctx, runtime, operationPing, http.MethodGet, requestURL, "")
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, operationPing))
		}
		return closeBody(resp.Body)
	})
	if err != nil {
		return err
	}
	release()
	return nil
}

func (c *Client) GetManifest(ctx context.Context, req GetManifestRequest) (*ManifestResponse, error) {
	var out *ManifestResponse
	release, err := c.doWithFailover(ctx, failoverRequest{alias: req.UpstreamAlias, operation: operationManifest}, func(runtime upstreamRuntime) error {
		method := methodOr(req.Method, http.MethodGet)
		requestURL := registryURL(runtime.config.Registry, req.Repo, endpointManifest, req.Reference)
		var opts []requestOption
		if req.Accept != "" {
			opts = append(opts, withHeader(distribution.HeaderAccept, req.Accept))
		}
		resp, err := c.do(ctx, runtime, operationManifest, method, requestURL, pullRepositoryScope(req.Repo), opts...)
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, operationManifest))
		}
		out = &ManifestResponse{
			Body:      resp.Body,
			Digest:    resp.Header.Get(distribution.HeaderDockerContentDigest),
			MediaType: contentType(resp.Header),
			Size:      contentLength(resp.Header),
			Headers:   resp.Header.Clone(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	release()
	return out, nil
}

func (c *Client) GetBlob(ctx context.Context, req GetBlobRequest) (*BlobResponse, error) {
	var out *BlobResponse
	release, err := c.doWithFailover(ctx, failoverRequest{alias: req.UpstreamAlias, operation: operationBlob, digest: req.Digest}, func(runtime upstreamRuntime) error {
		method := methodOr(req.Method, http.MethodGet)
		requestURL := registryURL(runtime.config.Registry, req.Repo, endpointBlob, req.Digest)
		var opts []requestOption
		if req.Range != nil {
			opts = append(opts, withHeader(distribution.HeaderRange, req.Range.String()))
		}
		resp, err := c.do(ctx, runtime, operationBlob, method, requestURL, pullRepositoryScope(req.Repo), opts...)
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, operationBlob))
		}
		out = &BlobResponse{
			Body:       resp.Body,
			Digest:     lo.CoalesceOrEmpty(resp.Header.Get(distribution.HeaderDockerContentDigest), req.Digest),
			Size:       contentLength(resp.Header),
			StatusCode: resp.StatusCode,
			Headers:    resp.Header.Clone(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil || out.Body == nil {
		release()
		return out, nil
	}
	out.Body = newReleaseReadCloser(out.Body, release)
	return out, nil
}

func (c *Client) ListTags(ctx context.Context, req ListTagsRequest) (*TagsResponse, error) {
	var out *TagsResponse
	release, err := c.doWithFailover(ctx, failoverRequest{alias: req.UpstreamAlias, operation: operationTags}, func(runtime upstreamRuntime) error {
		requestURL, err := tagsURL(runtime.config.Registry, req)
		if err != nil {
			return err
		}

		resp, err := c.do(ctx, runtime, operationTags, http.MethodGet, requestURL, pullRepositoryScope(req.Repo))
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, operationTags))
		}
		out = &TagsResponse{Body: resp.Body, Headers: resp.Header.Clone()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	release()
	return out, nil
}

func (c *Client) GetReferrers(ctx context.Context, req ReferrersRequest) (*ReferrersResponse, error) {
	var out *ReferrersResponse
	release, err := c.doWithFailover(ctx, failoverRequest{alias: req.UpstreamAlias, operation: operationReferrers}, func(runtime upstreamRuntime) error {
		requestURL := registryURL(runtime.config.Registry, req.Repo, endpointReferrers, req.Digest)
		resp, err := c.do(ctx, runtime, operationReferrers, http.MethodGet, requestURL, pullRepositoryScope(req.Repo))
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, operationReferrers))
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
	release()
	return out, nil
}
