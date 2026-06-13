package dist

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (s *Service) responseFromStored(req Request, requestRoute Route, stored storedResponse, cacheStatus string) (*Response, error) {
	headers := cacheHeaders(stored.headers, stored.size)
	for key, values := range stored.headers {
		if key == distribution.HeaderContentRange {
			headers[key] = slices.Clone(values)
		}
	}
	headers.Set(headerMirrorCache, cacheStatus)
	headers.Set("Accept-Ranges", distribution.RangeUnitBytes)
	status := stored.status
	if status == 0 {
		status = http.StatusOK
	}
	body := stored.body
	if methodOrGet(req.Method) == http.MethodHead && body != nil {
		if err := body.Close(); err != nil {
			return nil, wrapError(err, "close cached dist object for head")
		}
		body = http.NoBody
	}
	return &Response{
		Status:      status,
		Headers:     headers,
		Body:        body,
		ContentType: routeContentType(requestRoute, headers),
		Size:        contentLength(headers, stored.size),
		Cache:       cacheStatus,
	}, nil
}

func (s *Service) responseFromUpstream(req Request, requestRoute Route, fetched *upstreamFetch) *Response {
	headers := cacheHeaders(fetched.headers, -1)
	headers.Set(headerMirrorCache, cacheMiss)
	body := fetched.body
	if methodOrGet(req.Method) == http.MethodHead && body != nil {
		closeReadCloser(body, s.logger, "close dist upstream body for head")
		body = http.NoBody
	}
	return &Response{
		Status:      fetched.status,
		Headers:     headers,
		Body:        body,
		ContentType: routeContentType(requestRoute, headers),
		Size:        contentLength(headers, -1),
		Cache:       cacheMiss,
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
		out.Set(distribution.HeaderContentLength, formatInt64(size))
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
		distribution.HeaderContentRange,
		"Last-Modified",
	}
}

func routeContentType(requestRoute Route, headers http.Header) string {
	if value := headers.Get(distribution.HeaderContentType); value != "" {
		return value
	}
	if strings.HasSuffix(strings.ToLower(requestRoute.Reference), ".zip") {
		return "application/zip"
	}
	return "application/octet-stream"
}

func contentLength(headers http.Header, fallback int64) int64 {
	value := headers.Get(distribution.HeaderContentLength)
	if value == "" {
		return fallback
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return size
}

func formatInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}

func artifactExpired(expiresAt, now time.Time) bool {
	return !expiresAt.IsZero() && !now.Before(expiresAt)
}
