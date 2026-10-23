package dist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func (s *Service) fetchDistAttempt(ctx context.Context, cfg config.UpstreamConfig, requestURL string, req Request) (*upstreamhttp.Response, error) {
	attemptCtx, cancel := context.WithCancel(ctx)
	timeout := cfg.HTTP.AttemptTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	timer := time.AfterFunc(timeout, cancel)
	response, err := s.doFetch(attemptCtx, cfg, requestURL, upstreamhttp.Request{
		Method: methodOrGet(req.Method), URL: requestURL, Headers: distRequestHeaders(req), Auth: cfg.Auth,
	})
	stopped := timer.Stop()
	if attemptErr := attemptCtx.Err(); attemptErr != nil {
		err = errors.Join(err, attemptErr)
	} else if !stopped {
		err = errors.Join(err, context.DeadlineExceeded)
	}
	if err != nil {
		cancel()
		if response != nil && response.Body != nil {
			err = errors.Join(err, response.Body.Close())
		}
		return nil, wrapError(err, "fetch dist response headers")
	}
	response.Body = upstreamhttp.WithIdleTimeout(response.Body, cancel, cfg.HTTP.Timeout)
	return response, nil
}

func distRequestHeaders(req Request) http.Header {
	headers := http.Header{"User-Agent": {defaultUserAgent}, "Accept-Encoding": {"identity"}}
	if methodOrGet(req.Method) == http.MethodGet && strings.TrimSpace(req.Range) != "" {
		headers.Set("Range", req.Range)
		if req.IfRange != "" {
			headers.Set("If-Range", req.IfRange)
		}
	}
	return headers
}

func (s *Service) recoverDistHTTP(ctx context.Context, cfg config.UpstreamConfig, requestURL string, req Request, response *upstreamhttp.Response) io.ReadCloser {
	if response.Status != http.StatusOK && response.Status != http.StatusPartialContent {
		return response.Body
	}
	return transfer.HTTPRecovery(ctx, methodOrGet(req.Method), response.Headers, response.Body, "",
		func(retryCtx context.Context, headers http.Header) (*transfer.Response, error) {
			nextReq := req
			nextReq.Range, nextReq.IfRange = headers.Get("Range"), headers.Get("If-Range")
			next, err := s.fetchDistAttempt(retryCtx, cfg, requestURL, nextReq)
			if err != nil {
				return nil, err
			}
			return &transfer.Response{Status: next.Status, Headers: next.Headers, Body: next.Body, Size: contentLength(next.Headers, -1)}, nil
		})
}

func selectDistRange(req Request, response *upstreamhttp.Response, body io.ReadCloser) (*upstreamFetch, error) {
	size, err := distBodySize(body, contentLength(response.Headers, -1))
	if err != nil {
		return nil, err
	}
	selected, err := transfer.ApplyRange(methodOrGet(req.Method), req.Range, req.IfRange, transfer.Response{
		Status: response.Status, Headers: response.Headers, Body: body, Size: size,
	})
	if err != nil {
		return nil, wrapError(err, "select dist response range")
	}
	selected.Headers.Set("Accept-Ranges", "bytes")
	return &upstreamFetch{status: selected.Status, headers: selected.Headers, body: selected.Body}, nil
}

func distBodySize(body io.ReadCloser, size int64) (int64, error) {
	if size >= 0 {
		return size, nil
	}
	seeker, ok := body.(io.Seeker)
	if !ok {
		return size, nil
	}
	size, err := seeker.Seek(0, io.SeekEnd)
	if err == nil {
		_, err = seeker.Seek(0, io.SeekStart)
	}
	if err != nil {
		return 0, errors.Join(fmt.Errorf("measure dist response: %w", err), body.Close())
	}
	return size, nil
}

func (s *Service) openRecoverableObject(ctx context.Context, key, digest string, fullSize int64, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	reader, info, err := transfer.OpenObject(ctx, s.objects, transfer.ObjectRequest{
		Key: key, Digest: digest, Size: fullSize, Options: opts,
	})
	if err != nil {
		return nil, nil, wrapError(err, "open recoverable dist object")
	}
	return reader, info, nil
}
