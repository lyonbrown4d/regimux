package pypi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/policy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const (
	cacheHit   = "hit"
	cacheMiss  = "miss"
	cacheStale = "stale"
)

func TestNormalizeProjectNameUsesPEP503(t *testing.T) {
	got, err := pypi.NormalizeProjectName("My_Package.Name")
	requireNoError(t, "normalize project", err)
	if got != "my-package-name" {
		t.Fatalf("normalized name = %q, want %q", got, "my-package-name")
	}
}

func TestServiceRewritesSimpleLinksAndCaches(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/simple/my-package/" {
			t.Fatalf("upstream path = %s, want /simple/my-package/", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		writeResponse(t, w, `<html><body><a href="`+upstreamPackageURL(r, "/packages/My_Package-1.0.0-py3-none-any.whl")+`#sha256=abc">wheel</a></body></html>`)
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(ctx, t, upstream.URL, nil)
	first, err := service.Get(ctx, pypi.Request{
		Alias: "pypi",
		Tail:  "simple/My_Package/",
	})
	requireNoError(t, "first simple get", err)
	body := readResponse(t, first)
	expectedHref := expectedLocalHref(t, "pypi", upstream.URL, "/packages/My_Package-1.0.0-py3-none-any.whl") + "#sha256=abc"
	if !strings.Contains(body, `href="`+expectedHref+`"`) {
		t.Fatalf("rewritten body %q does not contain href %q", body, expectedHref)
	}
	if first.Cache != cacheMiss {
		t.Fatalf("first cache = %q, want %q", first.Cache, cacheMiss)
	}

	second, err := service.Get(ctx, pypi.Request{
		Alias: "pypi",
		Tail:  "simple/My_Package/",
	})
	requireNoError(t, "second simple get", err)
	if got := readResponse(t, second); got != body {
		t.Fatalf("second body = %q, want %q", got, body)
	}
	if second.Cache != cacheHit {
		t.Fatalf("second cache = %q, want %q", second.Cache, cacheHit)
	}
	if requests != 1 {
		t.Fatalf("upstream requests = %d, want 1", requests)
	}
}

func TestServiceServesExpiredSimpleIndexStaleWithoutRefreshingInline(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/simple/demo/" {
			t.Fatalf("upstream path = %s, want /simple/demo/", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		writeResponse(t, w, "<a href='/packages/demo-"+strconv.Itoa(requests)+".tar.gz'>sdist</a>")
	}))
	t.Cleanup(upstream.Close)

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	service := newTestService(ctx, t, upstream.URL, func() time.Time { return now })
	first, err := service.Get(ctx, pypi.Request{
		Alias: "pypi",
		Tail:  "simple/demo/",
	})
	requireNoError(t, "first simple get", err)
	firstBody := readResponse(t, first)

	now = now.Add(6 * time.Minute)
	second, err := service.Get(ctx, pypi.Request{
		Alias: "pypi",
		Tail:  "simple/demo/",
	})
	requireNoError(t, "second simple get", err)
	secondBody := readResponse(t, second)
	if second.Cache != cacheStale {
		t.Fatalf("second cache = %q, want %q", second.Cache, cacheStale)
	}
	if secondBody != firstBody {
		t.Fatalf("second body = %q, want stale body %q", secondBody, firstBody)
	}
	if requests != 1 {
		t.Fatalf("upstream requests after stale hit = %d, want 1", requests)
	}
}

func TestServiceCachesPackageFileLongTerm(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/packages/demo-1.0.0.tar.gz" {
			t.Fatalf("upstream path = %s, want /packages/demo-1.0.0.tar.gz", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/gzip")
		writeResponse(t, w, "sdist-bytes")
	}))
	t.Cleanup(upstream.Close)

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	service := newTestService(ctx, t, upstream.URL, func() time.Time { return now })
	tail := packageTailFor(t, upstream.URL, "/packages/demo-1.0.0.tar.gz")
	first, err := service.Get(ctx, pypi.Request{Alias: "pypi", Tail: tail})
	requireNoError(t, "first package get", err)
	if got := readResponse(t, first); got != "sdist-bytes" {
		t.Fatalf("first body = %q, want sdist-bytes", got)
	}

	now = now.Add(24 * time.Hour)
	second, err := service.Get(ctx, pypi.Request{Alias: "pypi", Tail: tail})
	requireNoError(t, "second package get", err)
	if got := readResponse(t, second); got != "sdist-bytes" {
		t.Fatalf("second body = %q, want sdist-bytes", got)
	}
	if second.Cache != cacheHit {
		t.Fatalf("second cache = %q, want %q", second.Cache, cacheHit)
	}
	if requests != 1 {
		t.Fatalf("upstream requests = %d, want 1", requests)
	}
}

func TestServicePersistsPackageFileAfterFullDownload(t *testing.T) {
	ctx := context.Background()
	const body = "sdist-bytes"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/packages/demo-1.0.0.tar.gz" {
			t.Fatalf("upstream path = %s, want /packages/demo-1.0.0.tar.gz", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/gzip")
		writeResponse(t, w, body)
	}))
	t.Cleanup(upstream.Close)

	service, metadata, objects := newTestServiceWithStores(ctx, t, upstream.URL, nil)
	tail := packageTailFor(t, upstream.URL, "/packages/demo-1.0.0.tar.gz")
	resp, err := service.Get(ctx, pypi.Request{Alias: "pypi", Tail: tail})
	requireNoError(t, "package get", err)
	if got := readResponse(t, resp); got != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
	if resp.Cache != cacheMiss {
		t.Fatalf("cache = %q, want %q", resp.Cache, cacheMiss)
	}

	assertStoredArtifact(ctx, t, metadata, objects, meta.TagKey{
		Alias:      "pypi",
		Repository: "pypi/packages",
		Reference:  strings.TrimPrefix(tail, "packages/"),
	}, body, "application/gzip")
}

func TestServiceBlockedByPolicyDoesNotFetchUpstream(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		requests++
		t.Fatal("upstream should not be called when policy blocks pypi request")
	}))
	t.Cleanup(upstream.Close)

	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", metadata.Close())
	})

	service := pypi.NewService(pypi.ServiceDependencies{
		Config: config.Config{
			PyPI: config.DependencyEcosystemConfig{
				"pypi": {Registry: upstream.URL},
			},
			Policy: config.PolicyConfig{
				Dependency: config.DependencyPolicyConfig{
					Block: []config.DependencyRuleConfig{
						{
							Ecosystem: "pypi",
							Alias:     "pypi",
							Artifact:  "pypi/simple/demo",
						},
					},
				},
			},
		},
		Metadata: metadata,
	})
	_, err = service.Get(ctx, pypi.Request{
		Alias: "pypi",
		Tail:  "simple/Demo/",
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
		Alias:      "pypi/pypi",
		Repository: "pypi/simple/demo",
		Reference:  "index.html",
	})
}

func TestServiceRejectsNonPyPIUpstream(t *testing.T) {
	ctx := context.Background()
	service := pypi.NewService(pypi.ServiceDependencies{
		Config: config.Config{
			Go: config.DependencyEcosystemConfig{
				"default": {Registry: "https://proxy.golang.org"},
			},
		},
	})
	_, err := service.Get(ctx, pypi.Request{
		Alias: "default",
		Tail:  "simple/demo/",
	})
	if err == nil {
		t.Fatal("expected non-pypi upstream error")
	}
}
