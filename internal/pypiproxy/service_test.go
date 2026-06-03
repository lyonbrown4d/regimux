package pypiproxy_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/pypiproxy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const (
	cacheHit  = "hit"
	cacheMiss = "miss"
)

func TestNormalizeProjectNameUsesPEP503(t *testing.T) {
	got, err := pypiproxy.NormalizeProjectName("My_Package.Name")
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
	first, err := service.Get(ctx, pypiproxy.Request{
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

	second, err := service.Get(ctx, pypiproxy.Request{
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

func TestServiceRefetchesExpiredSimpleIndex(t *testing.T) {
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
	first, err := service.Get(ctx, pypiproxy.Request{
		Alias: "pypi",
		Tail:  "simple/demo/",
	})
	requireNoError(t, "first simple get", err)
	firstBody := readResponse(t, first)

	now = now.Add(6 * time.Minute)
	second, err := service.Get(ctx, pypiproxy.Request{
		Alias: "pypi",
		Tail:  "simple/demo/",
	})
	requireNoError(t, "second simple get", err)
	secondBody := readResponse(t, second)
	if firstBody == secondBody {
		t.Fatalf("simple index was not refetched after ttl: %q", secondBody)
	}
	if requests != 2 {
		t.Fatalf("upstream requests = %d, want 2", requests)
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
	first, err := service.Get(ctx, pypiproxy.Request{Alias: "pypi", Tail: tail})
	requireNoError(t, "first package get", err)
	if got := readResponse(t, first); got != "sdist-bytes" {
		t.Fatalf("first body = %q, want sdist-bytes", got)
	}

	now = now.Add(24 * time.Hour)
	second, err := service.Get(ctx, pypiproxy.Request{Alias: "pypi", Tail: tail})
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

func TestServiceRejectsNonPyPIUpstream(t *testing.T) {
	ctx := context.Background()
	service := pypiproxy.NewService(pypiproxy.ServiceDependencies{
		Config: config.Config{
			Go: config.DependencyEcosystemConfig{
				"default": {Registry: "https://proxy.golang.org"},
			},
		},
	})
	_, err := service.Get(ctx, pypiproxy.Request{
		Alias: "default",
		Tail:  "simple/demo/",
	})
	if err == nil {
		t.Fatal("expected non-pypi upstream error")
	}
}

func newTestService(ctx context.Context, t *testing.T, upstreamURL string, now func() time.Time) *pypiproxy.Service {
	t.Helper()
	db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", db.Close())
	})
	objects, err := object.NewMemory("pypi-proxy-test")
	requireNoError(t, "open objects", err)
	return pypiproxy.NewService(pypiproxy.ServiceDependencies{
		Config: config.Config{
			PyPI: config.DependencyEcosystemConfig{
				"pypi": {
					Registry: upstreamURL,
				},
			},
		},
		Metadata: db,
		Objects:  objects,
		Now:      now,
	})
}

func upstreamPackageURL(r *http.Request, path string) string {
	return "http://" + r.Host + path
}

func expectedLocalHref(t *testing.T, alias, upstreamURL, path string) string {
	t.Helper()
	parsed, err := url.Parse(upstreamURL)
	requireNoError(t, "parse upstream url", err)
	return "/pypi/" + alias + "/packages/" + parsed.Scheme + "/" + parsed.Host + path
}

func packageTailFor(t *testing.T, upstreamURL, path string) string {
	t.Helper()
	parsed, err := url.Parse(upstreamURL)
	requireNoError(t, "parse upstream url", err)
	return "packages/" + parsed.Scheme + "/" + parsed.Host + "/" + strings.TrimLeft(path, "/")
}

func readResponse(t *testing.T, resp *pypiproxy.Response) string {
	t.Helper()
	if resp == nil || resp.Body == nil {
		t.Fatalf("response body is empty")
	}
	defer closeBody(t, resp.Body)
	body, err := io.ReadAll(resp.Body)
	requireNoError(t, "read body", err)
	return string(body)
}

func writeResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := strings.NewReader(body).WriteTo(w); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func closeBody(t *testing.T, body io.Closer) {
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
