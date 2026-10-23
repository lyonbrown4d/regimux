package pypi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestSimpleIndexCacheWriteProgressImmediatelyHitsHTTP(t *testing.T) {
	for _, adapter := range []struct {
		name  string
		start func(*testing.T, *pypi.Service) (*http.Client, string)
	}{
		{name: "std", start: startStdProxy},
		{name: "fiber", start: startFiberProxy},
	} {
		t.Run(adapter.name, func(t *testing.T) {
			fixture := newSimpleIndexWriteFixture(t)
			client, proxyURL := adapter.start(t, fixture.service)
			client.Timeout = 3 * time.Second
			for _, cache := range []string{cacheMiss, cacheHit} {
				fixture.assertHTTPDownload(t, client, proxyURL+"/pypi/pypi/simple/demo/", cache)
			}
			fixture.assertWrites(t)
		})
	}
}

func newSimpleIndexWriteFixture(t *testing.T) *cacheWriteFixture {
	t.Helper()
	body := "<html><body>" + strings.Repeat("x", (128<<10)-26) + "</body></html>"
	upstream, calls := newSimpleBudgetUpstream(t, body)
	_, metadata, objects := newTestServiceWithStores(t.Context(), t, upstream.URL, nil)
	route, err := pypi.ParseTail("pypi", "simple/demo/")
	requireNoError(t, "parse simple index", err)
	digest := sha256.Sum256([]byte(body))
	sha := hex.EncodeToString(digest[:])
	store := &progressingPackageWrites{
		Store: objects, t: t, metadata: metadata, digest: "sha256:" + sha,
		key: meta.TagKey{Alias: route.Alias, Repository: route.Repository, Reference: route.Reference},
	}
	service := pypi.NewService(pypi.ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstream.URL}}},
		Metadata: metadata, Objects: store,
	})
	return &cacheWriteFixture{service: service, sha: sha, size: int64(len(body)), calls: calls, store: store}
}

func newSimpleBudgetUpstream(t *testing.T, body string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		writeResponse(t, w, body)
	}))
	t.Cleanup(upstream.Close)
	return upstream, &calls
}
