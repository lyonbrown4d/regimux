package npm

import (
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/spool"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

func materializeBody(body io.ReadCloser) (io.ReadCloser, error) {
	materialized, err := spool.MaterializeReadCloser(body, "regimux-npm-upstream-*")
	if err != nil {
		return nil, wrapError(err, "materialize npm proxy upstream body")
	}
	return materialized, nil
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

func closeReadCloser(body io.Closer, logger *slog.Logger, message string) {
	if body == nil {
		return
	}
	if err := body.Close(); err != nil && logger != nil {
		logger.Warn(message+" failed", "error", err)
	}
}

func requestMethod(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return http.MethodGet
	}
	return value
}

func urlWithQuery(rawURL, rawQuery string) string {
	rawQuery = strings.TrimPrefix(strings.TrimSpace(rawQuery), "?")
	if rawQuery == "" {
		return rawURL
	}
	if strings.Contains(rawURL, "?") {
		return rawURL + "&" + rawQuery
	}
	return rawURL + "?" + rawQuery
}

func requestPublicURL(servicePublicURL, requestedPublicURL string) string {
	if value := strings.TrimSpace(requestedPublicURL); value != "" {
		return strings.TrimRight(value, "/")
	}
	return servicePublicURL
}

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("npm").Wrapf(err, "%s", message)
}
