package upstream

import (
	"context"
	"net/http"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (c *Client) ConsumeBlob(ctx context.Context, req GetBlobRequest, consume BlobConsumeFunc) error {
	if consume == nil {
		return newError("blob consumer is not configured")
	}
	release, err := c.doWithFailover(ctx, blobConsumerFailoverRequest(req), func(runtime upstreamRuntime) error {
		blob, err := c.fetchBlob(ctx, runtime, req)
		if err != nil {
			return err
		}
		return consumeBlobResponse(blob, consume)
	})
	if err != nil {
		return err
	}
	release()
	return nil
}

func (c *Client) fetchBlob(ctx context.Context, runtime upstreamRuntime, req GetBlobRequest) (*BlobResponse, error) {
	method := methodOr(req.Method, http.MethodGet)
	requestURL := registryURL(runtime.config.Registry, req.Repo, endpointBlob, req.Digest)
	opts := blobRequestOptions(req)
	resp, err := c.do(ctx, runtime, requestSpec{
		operation: operationBlob,
		method:    method,
		endpoint:  requestURL,
		scope:     pullRepositoryScope(req.Repo),
		options:   opts,
	})
	if err != nil {
		return nil, err
	}
	return blobResponseFromUpstream(resp, req.Digest)
}

func blobFailoverRequest(req GetBlobRequest) failoverRequest {
	return failoverRequest{
		alias:      req.UpstreamAlias,
		operation:  operationBlob,
		repository: req.Repo,
		digest:     req.Digest,
	}
}

func blobConsumerFailoverRequest(req GetBlobRequest) failoverRequest {
	out := blobFailoverRequest(req)
	out.sequential = true
	return out
}

func blobRequestOptions(req GetBlobRequest) []requestOption {
	if req.Range == nil {
		return nil
	}
	return []requestOption{withHeader(distribution.HeaderRange, req.Range.String())}
}

func consumeBlobResponse(blob *BlobResponse, consume BlobConsumeFunc) error {
	if blob == nil || blob.Body == nil {
		return newError("upstream blob response body is empty")
	}
	consumeErr := consume(blob)
	closeErr := closeBody(blob.Body)
	if consumeErr != nil {
		return joinError(consumeErr, closeErr)
	}
	return closeErr
}
