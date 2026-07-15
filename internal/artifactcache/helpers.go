package artifactcache

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
)

var (
	errStoreNotConfigured = errors.New("artifact cache store is not configured")
	errEmptyBody          = errors.New("artifact body is empty")
)

func hashToTemp(tmp *os.File, tmpName string, source io.Reader) (string, int64, error) {
	digester := ocidigest.SHA256.Digester()
	size, err := io.Copy(io.MultiWriter(tmp, digester.Hash()), source)
	if err != nil {
		return "", 0, closeAndRemoveTemp(tmp, tmpName, err, "write artifact temp file")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", 0, closeAndRemoveTemp(tmp, tmpName, err, "rewind artifact temp file")
	}
	return digester.Digest().String(), size, nil
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
		"X-Regimux-Upstream",
	}
}

func closeAndRemoveTemp(file *os.File, name string, err error, message string) error {
	closeErr := file.Close()
	removeErr := os.Remove(name)
	return wrapError(errors.Join(err, closeErr, removeErr), message)
}

func removePath(path string, logger *slog.Logger) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && logger != nil {
		logger.Warn("remove temporary file failed", "path", path, "error", err)
	}
}
