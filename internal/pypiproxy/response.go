package pypiproxy

import (
	"net/http"
	"strconv"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (s *Service) responseFromStored(req Request, requestRoute Route, stored storedResponse, cacheStatus string) (*Response, error) {
	headers := cacheHeaders(stored.headers, stored.size)
	headers.Set(headerMirrorCache, cacheStatus)
	body := stored.body
	if methodOrGet(req.Method) == http.MethodHead && body != nil {
		if err := body.Close(); err != nil {
			return nil, wrapError(err, "close cached pypi object for head")
		}
		body = http.NoBody
	}
	return &Response{
		Status:      http.StatusOK,
		Headers:     headers,
		Body:        body,
		ContentType: routeContentType(requestRoute, headers),
		Size:        stored.size,
		Cache:       cacheStatus,
	}, nil
}

func (s *Service) responseFromUpstream(req Request, requestRoute Route, fetched *upstreamFetch) *Response {
	headers := cacheHeaders(fetched.headers, -1)
	headers.Set(headerMirrorCache, cacheMiss)
	body := fetched.body
	if methodOrGet(req.Method) == http.MethodHead && body != nil {
		closeReadCloser(body, s.logger, "close pypi upstream body for head")
		body = http.NoBody
	}
	return &Response{
		Status:      fetched.status,
		Headers:     headers,
		Body:        body,
		ContentType: routeContentType(requestRoute, headers),
		Size:        contentLength(headers),
		Cache:       cacheMiss,
	}
}

func cacheHeaders(headers http.Header, size int64) http.Header {
	out := http.Header{}
	for _, key := range cacheHeaderKeys() {
		if values, ok := headers[key]; ok {
			out[key] = append([]string(nil), values...)
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

func routeContentType(requestRoute Route, headers http.Header) string {
	if value := headers.Get(distribution.HeaderContentType); value != "" {
		return value
	}
	if requestRoute.Kind == RouteSimple {
		return "text/html; charset=utf-8"
	}
	return "application/octet-stream"
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
