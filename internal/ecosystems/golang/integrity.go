package golang

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func validateGoProxyBody(
	requestRoute route,
	body io.ReaderAt,
	size int64,
	headers http.Header,
) error {
	if isValidEmptyVersionList(requestRoute, size, headers) {
		return nil
	}
	if err := artifactcache.ValidateBody(
		body,
		size,
		headers,
		goProxyBodyValidator(requestRoute),
	); err != nil {
		return wrapError(err, "validate go proxy response")
	}
	return nil
}

func goProxyBodyValidator(requestRoute route) artifactcache.BodyValidator {
	reference := strings.ToLower(strings.TrimSpace(requestRoute.Reference))
	if strings.HasSuffix(reference, ".zip") {
		return artifactcache.ValidateZIP
	}
	return nil
}

func isValidEmptyVersionList(
	requestRoute route,
	size int64,
	headers http.Header,
) bool {
	if requestRoute.Reference != "@v/list" || size != 0 {
		return false
	}
	contentLength := strings.TrimSpace(headers.Get(distribution.HeaderContentLength))
	if contentLength == "" {
		return true
	}
	declared, err := strconv.ParseInt(contentLength, 10, 64)
	return err == nil && declared == 0
}
