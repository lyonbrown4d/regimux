package npm_test

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
	"github.com/lyonbrown4d/regimux/internal/ecosystems/npm"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestMetadataRefillsLegacyCacheWithoutURLRewriteFormat(t *testing.T) {
	const filename = "demo-1.0.0.tgz"
	var requests atomic.Int64
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/npm/demo" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, fmt.Sprintf(`{"name":"demo","versions":{"1.0.0":{"dist":{"tarball":%q}}}}`, upstream.URL+"/npm/demo/-/"+filename))
	}))
	t.Cleanup(upstream.Close)

	db, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() { requireNoError(t, "close metadata", db.Close()) })
	objects, err := object.NewLocal(t.TempDir())
	requireNoError(t, "open objects", err)
	cache := artifactcache.New(artifactcache.Dependencies{Metadata: db, Objects: objects})
	seed, err := cache.Put(t.Context(), artifactcache.PutRequest{
		Key:         artifactcache.Key{Alias: "npmjs", Repository: "demo", Reference: "metadata"},
		AcceptKey:   "npm",
		Body:        io.NopCloser(strings.NewReader(`{"name":"demo","versions":{"1.0.0":{"dist":{"tarball":"/npm/npmjs/npm/demo/-/demo-1.0.0.tgz"}}}}`)),
		Headers:     http.Header{"Content-Type": []string{"application/json"}},
		ContentType: "application/json",
		TTL:         time.Hour,
	})
	requireNoError(t, "seed legacy metadata", err)
	closeTestBody(t, seed.Body)
	service := npm.NewService(npm.ServiceDependencies{
		Config:   config.Config{NPM: config.DependencyEcosystemConfig{"npmjs": {Registry: upstream.URL + "/npm"}}},
		Metadata: db,
		Objects:  objects,
		Cache:    cache,
	})

	first, err := service.Get(t.Context(), npm.Request{Alias: "npmjs", Tail: "demo", SkipPullRecord: true})
	requireNoError(t, "refill legacy metadata", err)
	if first.Cache != "miss" {
		t.Fatalf("first cache status = %q, want miss after incompatible legacy cache", first.Cache)
	}
	firstDoc := readJSON(t, first)
	assertTarball(t, firstDoc, "1.0.0", "/npm/npmjs/demo/-/"+filename)
	if first.Headers.Get("X-Regimux-Metadata-Format") != "" {
		t.Fatalf("private metadata format marker leaked in response headers: %q", first.Headers.Get("X-Regimux-Metadata-Format"))
	}

	second, err := service.Get(t.Context(), npm.Request{Alias: "npmjs", Tail: "demo", SkipPullRecord: true})
	requireNoError(t, "read repaired metadata", err)
	if second.Cache != "hit" {
		t.Fatalf("second cache status = %q, want hit", second.Cache)
	}
	assertTarball(t, readJSON(t, second), "1.0.0", "/npm/npmjs/demo/-/"+filename)
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want one repair fetch", got)
	}
}
