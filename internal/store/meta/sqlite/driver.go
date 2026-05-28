// Package sqlite contains SQLite-specific metadata store driver helpers.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const busyTimeoutMillis = "10000"

func DSN(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("sqlite path is required")
	}
	if strings.EqualFold(path, ":memory:") {
		return path, nil
	}
	if IsMemoryOrFileURI(path) {
		return appendSQLitePragmas(path), nil
	}
	return appendSQLitePragmas(filepath.Clean(path)), nil
}

func EnsureDirectory(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || IsMemoryOrFileURI(path) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(path)), 0o750); err != nil {
		return fmt.Errorf("create sqlite metadata directory: %w", err)
	}
	return nil
}

func appendSQLitePragmas(dsn string) string {
	values := url.Values{}
	values.Add("_pragma", "busy_timeout="+busyTimeoutMillis)
	values.Add("_pragma", "journal_mode(WAL)")
	values.Add("_pragma", "synchronous(NORMAL)")
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + values.Encode()
}

func ConfigurePragmas(ctx context.Context, raw *sql.DB) error {
	if raw == nil {
		return errors.New("sqlite db is required")
	}
	statements := []string{
		"PRAGMA busy_timeout = " + busyTimeoutMillis,
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}
	for _, statement := range statements {
		if _, err := raw.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite metadata pragma: %w", err)
		}
	}
	return nil
}

func IsMemoryOrFileURI(path string) bool {
	return strings.EqualFold(path, ":memory:") || strings.HasPrefix(strings.ToLower(path), "file:")
}
