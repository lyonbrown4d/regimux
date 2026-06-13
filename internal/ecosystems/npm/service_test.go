package npm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/npm"
	"github.com/lyonbrown4d/regimux/internal/policy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const (
	cacheHit   = "hit"
	cacheMiss  = "miss"
	cacheStale = "stale"
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

func TestServiceBlockedByPolicyDoesNotFetchUpstream(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		requests++
		t.Fatal("upstream should not be called when policy blocks npm request")
	}))
	t.Cleanup(upstream.Close)

	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", metadata.Close())
	})

	service := npm.NewService(npm.ServiceDependencies{
		Config: config.Config{
			NPM: config.DependencyEcosystemConfig{
				"npmjs": {Registry: upstream.URL},
			},
			Policy: config.PolicyConfig{
				Dependency: config.DependencyPolicyConfig{
					Block: []config.DependencyRuleConfig{
						{
							Ecosystem: "npm",
							Alias:     "npmjs",
							Artifact:  "left-pad",
						},
					},
				},
			},
		},
		Metadata: metadata,
	})
	_, err = service.Get(ctx, npm.Request{
		Alias: "npmjs",
		Tail:  "left-pad",
	})
	if err == nil {
		t.Fatal("expected policy block error")
	}
	if !errors.Is(err, policy.ErrDependencyBlocked) {
		t.Fatalf("unexpected error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("upstream requests = %d, want 0", requests)
	}
	assertPolicyDeniedPull(ctx, t, metadata, meta.PullKey{
		Alias:      "npm/npmjs",
		Repository: "left-pad",
		Reference:  "metadata",
	})
}

func TestServicePersistsTarballAfterFullDownload(t *testing.T) {
	ctx := context.Background()
	const body = "tgz-bytes"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/left-pad/-/left-pad-1.0.0.tgz" {
			t.Fatalf("upstream path = %s, want tarball path", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		writeResponse(t, w, body)
	}))
	t.Cleanup(upstream.Close)

	service, metadata, objects := newTestServiceWithStores(ctx, t, upstream.URL, 5*time.Minute)
	resp, err := service.Get(ctx, npm.Request{
		Alias: "npmjs",
		Tail:  "left-pad/-/left-pad-1.0.0.tgz",
	})
	requireNoError(t, "tarball get", err)
	if got := readBody(t, resp); got != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
	if resp.Cache != cacheMiss {
		t.Fatalf("cache = %q, want %q", resp.Cache, cacheMiss)
	}

	assertStoredArtifact(ctx, t, metadata, objects, meta.TagKey{
		Alias:      "npmjs",
		Repository: "left-pad",
		Reference:  "tarball:left-pad-1.0.0.tgz",
	}, body, "application/octet-stream")
}

func TestServiceServesExpiredMetadataStaleWithoutRefreshingInline(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, `{"name":"left-pad","versions":{"1.0.0":{"dist":{"tarball":"https://registry.npmjs.org/left-pad/-/left-pad-`+strconv.Itoa(requests)+`.0.0.tgz"}}}}`)
	}))
	t.Cleanup(upstream.Close)

	service, metadata := newTestServiceWithMetadata(ctx, t, upstream.URL, 5*time.Minute)
	first, err := service.Get(ctx, npm.Request{Alias: "npmjs", Tail: "left-pad"})
	requireNoError(t, "first metadata get", err)
	_ = readJSON(t, first)
	expireArtifactMetadata(ctx, t, metadata, "npmjs", "left-pad", "metadata")
	second, err := service.Get(ctx, npm.Request{Alias: "npmjs", Tail: "left-pad"})
	requireNoError(t, "second metadata get", err)
	_ = readJSON(t, second)
	if second.Cache != cacheStale {
		t.Fatalf("second cache = %q, want %q", second.Cache, cacheStale)
	}
	if requests != 1 {
		t.Fatalf("upstream requests after stale hit = %d, want 1", requests)
	}
}
