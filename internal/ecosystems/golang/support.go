package golang

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/samber/oops"
)

func methodOr(value, fallback string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func expiredAt(expiresAt, now time.Time) bool {
	return !expiresAt.IsZero() && !now.Before(expiresAt)
}

func closeReadCloser(body io.ReadCloser, logger *slog.Logger, message string) {
	if body == nil {
		return
	}
	if err := body.Close(); err != nil && logger != nil {
		logger.Warn(message+" failed", "error", err)
	}
}

func removePath(path string, logger *slog.Logger) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && logger != nil {
		logger.Warn("remove temporary file failed", "path", path, "error", err)
	}
}

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("go-proxy").Wrapf(err, "%s", message)
}
