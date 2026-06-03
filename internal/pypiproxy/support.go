package pypiproxy

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
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

func closeAndRemoveTemp(file *os.File, name string, err error, message string) error {
	closeErr := file.Close()
	removeErr := os.Remove(name)
	return wrapError(errors.Join(err, closeErr, removeErr), message)
}

func requestPublicURL(servicePublicURL, requestedPublicURL string) string {
	if value := strings.TrimSpace(requestedPublicURL); value != "" {
		return strings.TrimRight(value, "/")
	}
	return servicePublicURL
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
	return oops.In("pypi-proxy").Wrapf(err, "%s", message)
}
