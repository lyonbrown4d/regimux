package artifactcache

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
)

var (
	errStoreNotConfigured = errors.New("artifact cache store is not configured")
	errEmptyBody          = errors.New("artifact body is empty")
)

func hashToTemp(tmp io.ReadWriteSeeker, source io.Reader) (string, int64, error) {
	digester := ocidigest.SHA256.Digester()
	size, err := io.Copy(io.MultiWriter(tmp, digester.Hash()), source)
	if err != nil {
		return "", 0, wrapError(err, "write artifact temp file")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", 0, wrapError(err, "rewind artifact temp file")
	}
	return digester.Digest().String(), size, nil
}

func cacheHeaders(headers http.Header, size int64) http.Header {
	out := http.Header{}
	for _, key := range cacheHeaderKeys() {
		values := headers.Values(key)
		if len(values) == 0 {
			values = headers[key]
		}
		if len(values) != 0 {
			out[http.CanonicalHeaderKey(key)] = slices.Clone(values)
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
		"X-Regimux-Metadata-Format",
	}
}
