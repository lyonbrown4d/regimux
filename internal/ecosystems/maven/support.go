package maven

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/samber/oops"
)

func methodOrGet(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return http.MethodGet
	}
	return value
}

func closeReadCloser(body io.Closer, logger *slog.Logger, message string) {
	if body == nil {
		return
	}
	if err := body.Close(); err != nil && logger != nil {
		logger.Warn(message+" failed", "error", err)
	}
}

func shouldPassThrough(req Request, status int) bool {
	return status < http.StatusOK ||
		status >= http.StatusMultipleChoices ||
		methodOrGet(req.Method) == http.MethodHead
}

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("maven").Wrapf(err, "%s", message)
}
