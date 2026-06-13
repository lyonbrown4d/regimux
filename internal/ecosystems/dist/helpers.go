package dist

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
)

func methodOrGet(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return http.MethodGet
	}
	return method
}

func closeReadCloser(closer io.ReadCloser, logger *slog.Logger, message string) {
	if closer == nil || closer == http.NoBody {
		return
	}
	if err := closer.Close(); err != nil && logger != nil {
		logger.Warn(message, "error", err)
	}
}
