// Package sqlite contains SQLite-specific metadata store driver helpers.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func DSN(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("sqlite path is required")
	}
	if IsMemoryOrFileURI(path) {
		return path, nil
	}
	return filepath.Clean(path), nil
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

func ConfigurePragmas(ctx context.Context, raw *sql.DB) error {
	if raw == nil {
		return errors.New("sqlite db is required")
	}
	statements := []string{
		"PRAGMA busy_timeout = 5000",
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
