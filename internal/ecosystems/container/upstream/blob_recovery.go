package upstream

import (
	"context"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func (c *Client) fetchRecoverableBlob(ctx context.Context, runtime upstreamRuntime, req GetBlobRequest) (*BlobResponse, error) {
	transferCtx, cancel := context.WithCancel(ctx)
	blob, err := c.requestBlobAttempt(transferCtx, runtime, req, blobRequestOptions(req))
	if err != nil {
		cancel()
		return nil, err
	}
	body := blob.Body
	if blob.StatusCode == http.StatusOK || blob.StatusCode == http.StatusPartialContent {
		body = transfer.HTTPRecovery(transferCtx, methodOr(req.Method, http.MethodGet), blob.Headers, body, req.Digest,
			func(retryCtx context.Context, headers http.Header) (*transfer.Response, error) {
				next, err := c.requestBlobAttempt(retryCtx, runtime, req, blobRecoveryOptions(headers))
				if err != nil {
					return nil, err
				}
				return &transfer.Response{Status: next.StatusCode, Headers: next.Headers, Body: next.Body, Size: next.Size}, nil
			})
	}
	blob.Body = transfer.CancelOnClose(body, cancel)
	return blob, nil
}

func (c *Client) requestBlobAttempt(ctx context.Context, runtime upstreamRuntime, req GetBlobRequest, opts []requestOption) (*BlobResponse, error) {
	attemptCtx, cancel := context.WithCancel(ctx)
	timeout := runtime.config.HTTP.AttemptTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	timer := time.AfterFunc(timeout, cancel)
	response, err := c.do(attemptCtx, runtime, requestSpec{
		operation: operationBlob,
		method:    methodOr(req.Method, http.MethodGet),
		endpoint:  registryURL(runtime.config.Registry, req.Repo, endpointBlob, req.Digest),
		scope:     pullRepositoryScope(req.Repo),
		options:   opts,
	})
	stopped := timer.Stop()
	if attemptErr := attemptCtx.Err(); attemptErr != nil {
		err = joinError(err, attemptErr)
	} else if !stopped {
		err = joinError(err, context.DeadlineExceeded)
	}
	if err != nil {
		cancel()
		return nil, closeBodyWithError(response.Body, err)
	}
	blob, err := blobResponseFromUpstream(response, req.Digest)
	if err != nil {
		cancel()
		return nil, err
	}
	blob.Body = upstreamhttp.WithIdleTimeout(blob.Body, cancel, runtime.config.HTTP.Timeout)
	return blob, nil
}

func blobRecoveryOptions(headers http.Header) []requestOption {
	opts := make([]requestOption, 0, len(headers))
	for key, values := range headers {
		for _, value := range values {
			opts = append(opts, withHeader(key, value))
		}
	}
	return opts
}
