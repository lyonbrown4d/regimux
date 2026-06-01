package upstream

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"sync"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"resty.dev/v3"
)

type upstreamResponse struct {
	Body       io.ReadCloser
	Header     http.Header
	StatusCode int
}

type releaseReadCloser struct {
	body    io.ReadCloser
	release func()
	once    sync.Once
}

func newReleaseReadCloser(body io.ReadCloser, release func()) io.ReadCloser {
	if body == nil || release == nil {
		return body
	}
	return &releaseReadCloser{body: body, release: release}
}

func (r *releaseReadCloser) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if err == nil {
		return n, nil
	}
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	return n, wrapError(err, "read upstream response body")
}

func (r *releaseReadCloser) Close() error {
	err := r.body.Close()
	r.once.Do(r.release)
	if err != nil {
		return wrapError(err, "close upstream response body")
	}
	return nil
}

func rawUpstreamResponse(resp *resty.Response) (upstreamResponse, error) {
	if resp == nil || resp.RawResponse == nil {
		return upstreamResponse{}, newError("upstream response is empty")
	}
	body := resp.RawResponse.Body
	if resp.Body != nil {
		body = resp.Body
	}
	return upstreamResponse{
		Body:       body,
		Header:     resp.RawResponse.Header,
		StatusCode: resp.RawResponse.StatusCode,
	}, nil
}

func closeBody(body io.Closer) error {
	if body == nil {
		return nil
	}
	if err := body.Close(); err != nil {
		return wrapError(err, "close upstream response body")
	}
	return nil
}

func closeBodyWithError(body io.Closer, err error) error {
	closeErr := closeBody(body)
	if closeErr != nil {
		return joinError(err, closeErr)
	}
	return err
}

func drainAndClose(body io.ReadCloser) error {
	if body == nil {
		return nil
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return joinError(
			wrapError(err, "drain upstream response body"),
			closeBody(body),
		)
	}
	return closeBody(body)
}

func contentType(header http.Header) string {
	value := header.Get(distribution.HeaderContentType)
	if value == "" {
		return distribution.MediaTypeOctetStream
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err == nil && mediaType != "" {
		return mediaType
	}
	return value
}

func contentLength(header http.Header) int64 {
	value := header.Get(distribution.HeaderContentLength)
	if value == "" {
		return -1
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return n
}
