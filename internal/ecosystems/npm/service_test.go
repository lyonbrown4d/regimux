package npm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/npm"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const (
	cacheHit  = "hit"
	cacheMiss = "miss"
)

func TestServiceRewritesMetadataTarballsAndCaches(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/left-pad" {
			t.Fatalf("upstream path = %s, want /left-pad", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, `{
			"name": "left-pad",
			"versions": {
				"1.0.0": {
					"dist": {
						"tarball": "`+upstreamTarballURL(r, "left-pad/-/left-pad-1.0.0.tgz")+`",
						"integrity": "sha512-test"
					}
				}
			}
		}`)
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL, 5*time.Minute)
	first, err := service.Get(ctx, npm.Request{
		Alias:        "npmjs",
		Tail:         "left-pad",
		ProxyBaseURL: "https://cache.example.test",
	})
	requireNoError(t, "first metadata get", err)
	if first.Cache != cacheMiss {
		t.Fatalf("first cache = %q, want %q", first.Cache, cacheMiss)
	}
	firstDoc := readJSON(t, first)
	assertTarball(t, firstDoc, "1.0.0", "https://cache.example.test/npm/npmjs/left-pad/-/left-pad-1.0.0.tgz")

	second, err := service.Get(ctx, npm.Request{
		Alias:        "npmjs",
		Tail:         "left-pad",
		ProxyBaseURL: "https://cache.example.test",
	})
	requireNoError(t, "second metadata get", err)
	if second.Cache != cacheHit {
		t.Fatalf("second cache = %q, want %q", second.Cache, cacheHit)
	}
	secondDoc := readJSON(t, second)
	assertTarball(t, secondDoc, "1.0.0", "https://cache.example.test/npm/npmjs/left-pad/-/left-pad-1.0.0.tgz")
	if requests != 1 {
		t.Fatalf("upstream requests = %d, want 1", requests)
	}
}

func TestServiceHandlesScopedMetadataPaths(t *testing.T) {
	for _, tail := range []string{"@scope/pkg", "@scope%2fpkg"} {
		t.Run(tail, func(t *testing.T) {
			ctx := context.Background()
			requests := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.RawPath != "/@scope%2fpkg" && r.RequestURI != "/@scope%2fpkg" {
					t.Fatalf("upstream request uri = %s, raw path = %s, want encoded scoped metadata", r.RequestURI, r.URL.RawPath)
				}
				w.Header().Set("Content-Type", "application/json")
				writeResponse(t, w, `{
					"name": "@scope/pkg",
					"versions": {
						"1.0.0": {
							"dist": {
								"tarball": "https://registry.npmjs.org/@scope/pkg/-/pkg-1.0.0.tgz"
							}
						}
					}
				}`)
			}))
			t.Cleanup(upstream.Close)

			service := newTestService(ctx, t, upstream.URL, 5*time.Minute)
			resp, err := service.Get(ctx, npm.Request{
				Alias: "npmjs",
				Tail:  tail,
			})
			requireNoError(t, "scoped metadata get", err)
			doc := readJSON(t, resp)
			assertTarball(t, doc, "1.0.0", "/npm/npmjs/@scope/pkg/-/pkg-1.0.0.tgz")
			if requests != 1 {
				t.Fatalf("upstream requests = %d, want 1", requests)
			}
		})
	}
}

func TestServiceCachesTarballWithoutTTL(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/@scope/pkg/-/pkg-1.0.0.tgz" {
			t.Fatalf("upstream path = %s, want scoped tarball path", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		writeResponse(t, w, "tgz-bytes")
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL, time.Nanosecond)
	for i := range 2 {
		resp, err := service.Get(ctx, npm.Request{
			Alias: "npmjs",
			Tail:  "@scope/pkg/-/pkg-1.0.0.tgz",
		})
		requireNoError(t, "tarball get", err)
		body := readBody(t, resp)
		if body != "tgz-bytes" {
			t.Fatalf("body = %q, want tgz-bytes", body)
		}
		wantCache := cacheMiss
		if i == 1 {
			wantCache = cacheHit
		}
		if resp.Cache != wantCache {
			t.Fatalf("cache = %q, want %q", resp.Cache, wantCache)
		}
		time.Sleep(time.Millisecond)
	}
	if requests != 1 {
		t.Fatalf("upstream requests = %d, want 1", requests)
	}
}

func TestServiceRefreshesExpiredMetadata(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, `{"name":"left-pad","versions":{"1.0.0":{"dist":{"tarball":"https://registry.npmjs.org/left-pad/-/left-pad-`+strconv.Itoa(requests)+`.0.0.tgz"}}}}`)
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL, time.Nanosecond)
	first, err := service.Get(ctx, npm.Request{Alias: "npmjs", Tail: "left-pad"})
	requireNoError(t, "first metadata get", err)
	_ = readJSON(t, first)
	time.Sleep(time.Millisecond)
	second, err := service.Get(ctx, npm.Request{Alias: "npmjs", Tail: "left-pad"})
	requireNoError(t, "second metadata get", err)
	_ = readJSON(t, second)
	if requests != 2 {
		t.Fatalf("upstream requests = %d, want 2", requests)
	}
}

func newTestService(ctx context.Context, t *testing.T, upstreamURL string, metadataTTL time.Duration) *npm.Service {
	t.Helper()
	db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", db.Close())
	})
	objects, err := object.NewMemory("npm-test")
	requireNoError(t, "open objects", err)
	return npm.NewService(npm.ServiceDependencies{
		Config: config.Config{
			NPM: config.DependencyEcosystemConfig{
				"npmjs": {
					Registry: upstreamURL,
				},
			},
		},
		Metadata:    db,
		Objects:     objects,
		MetadataTTL: metadataTTL,
	})
}

func readJSON(t *testing.T, resp *npm.Response) map[string]any {
	t.Helper()
	body := readBody(t, resp)
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("decode response json: %v\nbody: %s", err, body)
	}
	return doc
}

func readBody(t *testing.T, resp *npm.Response) string {
	t.Helper()
	if resp == nil || resp.Body == nil {
		t.Fatal("response body is empty")
	}
	defer closeTestBody(t, resp.Body)
	body, err := io.ReadAll(resp.Body)
	requireNoError(t, "read body", err)
	return string(body)
}

func assertTarball(t *testing.T, doc map[string]any, version, want string) {
	t.Helper()
	versions, ok := doc["versions"].(map[string]any)
	if !ok {
		t.Fatalf("versions missing in %#v", doc)
	}
	versionDoc, ok := versions[version].(map[string]any)
	if !ok {
		t.Fatalf("version %s missing in %#v", version, versions)
	}
	dist, ok := versionDoc["dist"].(map[string]any)
	if !ok {
		t.Fatalf("dist missing in %#v", versionDoc)
	}
	if got := dist["tarball"]; got != want {
		t.Fatalf("tarball = %q, want %q", got, want)
	}
}

func upstreamTarballURL(r *http.Request, tail string) string {
	return "https://" + r.Host + "/" + tail
}

func writeResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := strings.NewReader(body).WriteTo(w); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func closeTestBody(t *testing.T, body io.Closer) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
}

func requireNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}
