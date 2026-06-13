package golang

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (s *Service) responseFromStored(req Request, stored storedResponse, cacheStatus string) (*Response, error) {
	headers := cacheHeaders(stored.headers, stored.size)
	headers.Set(headerMirrorCache, cacheStatus)
	if req.Method == http.MethodHead && stored.body != nil {
		if err := stored.body.Close(); err != nil {
			return nil, wrapError(err, "close cached go proxy object for head")
		}
		stored.body = http.NoBody
	}
	return &Response{
		Status:      http.StatusOK,
		Headers:     headers,
		Body:        stored.body,
		ContentType: contentType(headers, ""),
		Size:        stored.size,
		Cache:       cacheStatus,
	}, nil
}

func (s *Service) responseFromUpstream(req Request, fetched *upstreamFetch) *Response {
	headers := cacheHeaders(fetched.headers, -1)
	headers.Set(headerMirrorCache, cacheMiss)
	body := fetched.body
	if req.Method == http.MethodHead && body != nil {
		if err := body.Close(); err != nil {
			s.logger.Warn("close go proxy upstream body for head failed", "error", err)
		}
		body = http.NoBody
	}
	size := contentLength(headers)
	return &Response{
		Status:      fetched.status,
		Headers:     headers,
		Body:        body,
		ContentType: contentType(headers, ""),
		Size:        size,
		Cache:       cacheMiss,
	}
}

func fallbackStatus(status int) bool {
	return status == http.StatusNotFound || status == http.StatusGone
}

func closeResponseBody(resp *Response) {
	if resp == nil || resp.Body == nil || resp.Body == http.NoBody {
		return
	}
	if err := resp.Body.Close(); err != nil {
		return
	}
}

func cacheHeaders(headers http.Header, size int64) http.Header {
	out := http.Header{}
	for _, key := range cacheHeaderKeys() {
		if values, ok := headers[key]; ok {
			out[key] = slices.Clone(values)
		}
	}
	if size >= 0 {
		out.Set(distribution.HeaderContentLength, strconv.FormatInt(size, 10))
	} else if value := headers.Get(distribution.HeaderContentLength); value != "" {
		out.Set(distribution.HeaderContentLength, value)
	}
	return out
}

func cacheHeaderKeys() []string {
	return []string{
		"Cache-Control",
		"Content-Disposition",
		"Content-Encoding",
		"Content-Language",
		distribution.HeaderContentType,
		distribution.HeaderETag,
		"Last-Modified",
	}
}

func contentType(headers http.Header, reference string) string {
	if value := headers.Get(distribution.HeaderContentType); value != "" {
		return value
	}
	switch {
	case strings.HasSuffix(reference, ".zip"):
		return "application/zip"
	case strings.HasSuffix(reference, ".info"):
		return distribution.MediaTypeJSON
	default:
		return "text/plain; charset=utf-8"
	}
}

func contentLength(headers http.Header) int64 {
	value := headers.Get(distribution.HeaderContentLength)
	if value == "" {
		return -1
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return size
}
