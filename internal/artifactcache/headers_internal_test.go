package artifactcache

import (
	"net/http"
	"testing"
)

func TestCacheHeadersPreservesETag(t *testing.T) {
	headers := http.Header{}
	headers.Set("ETag", `"artifact-v1"`)
	if got := cacheHeaders(headers, 10).Get("ETag"); got != `"artifact-v1"` {
		t.Fatalf("ETag = %q", got)
	}
}
