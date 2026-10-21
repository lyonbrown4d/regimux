package pypi_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestSimpleIndexRefillsLegacyCacheWithoutURLRewriteFormat(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/pypi/simple/demo/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		writeResponse(t, w, fmt.Sprintf(`<a href="/pypi/files/demo-1.0.0-py3-none-any.whl#sha256=%s">wheel</a>`, digest))
	}))
	t.Cleanup(upstream.Close)

	db, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() { requireNoError(t, "close metadata", db.Close()) })
	objects, err := object.NewLocal(t.TempDir())
	requireNoError(t, "open objects", err)
	cache := artifactcache.New(artifactcache.Dependencies{Metadata: db, Objects: objects})
	seed, err := cache.Put(t.Context(), artifactcache.PutRequest{
		Key:         artifactcache.Key{Alias: "pypi", Repository: "pypi/simple/demo", Reference: "index.html"},
		AcceptKey:   "pypi",
		Body:        io.NopCloser(strings.NewReader(`<a href="/pypi/files/demo-1.0.0-py3-none-any.whl#sha256=` + digest + `">wheel</a>`)),
		Headers:     http.Header{"Content-Type": []string{"text/html"}},
		ContentType: "text/html",
		TTL:         time.Hour,
	})
	requireNoError(t, "seed legacy simple index", err)
	closeBody(t, seed.Body)
	service := pypi.NewService(pypi.ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstream.URL + "/pypi"}}},
		Metadata: db,
		Objects:  objects,
		Cache:    cache,
	})

	first, err := service.Get(t.Context(), pypi.Request{Alias: "pypi", Tail: "simple/demo", SkipPullRecord: true})
	requireNoError(t, "refill legacy simple index", err)
	if first.Cache != "miss" {
		t.Fatalf("first cache status = %q, want miss after incompatible legacy cache", first.Cache)
	}
	wantHref := "/pypi/pypi/packages/http/" + strings.TrimPrefix(upstream.URL, "http://") + "/pypi/files/demo-1.0.0-py3-none-any.whl"
	if !strings.Contains(readResponse(t, first), `href="`+wantHref+`?__regimux_project=demo&__regimux_sha256=`+digest+`#sha256=`+digest+`"`) {
		t.Fatalf("repaired simple index does not contain expected local wheel URL")
	}
	if first.Headers.Get("X-Regimux-Metadata-Format") != "" {
		t.Fatalf("private metadata format marker leaked in response headers: %q", first.Headers.Get("X-Regimux-Metadata-Format"))
	}

	second, err := service.Get(t.Context(), pypi.Request{Alias: "pypi", Tail: "simple/demo", SkipPullRecord: true})
	requireNoError(t, "read repaired simple index", err)
	if second.Cache != "hit" {
		t.Fatalf("second cache status = %q, want hit", second.Cache)
	}
	if !strings.Contains(readResponse(t, second), wantHref) {
		t.Fatalf("cached simple index lost repaired local wheel URL")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want one repair fetch", got)
	}
}
