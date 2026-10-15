//nolint:testpackage // White-box tests exercise the unexported resolver contract.
package pypi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestResolvePackageMirrorsReturnsOnlyExactTrustedArtifacts(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var primaryHits, mirrorHits atomic.Int32
	primary := httptest.NewServer(mirrorResolverPrimaryIndex(t, &primaryHits))
	defer primary.Close()
	mirror := httptest.NewServer(mirrorResolverMatchingIndex(t, &mirrorHits, digest))
	defer mirror.Close()

	service := &Service{client: http.DefaultClient}
	cfg := config.UpstreamConfig{
		Registry: primary.URL + "/simple",
		Mirrors:  []string{mirror.URL},
		Auth:     config.AuthConfig{Type: "basic", Username: "index-user", Password: "index-password"},
	}
	got := service.resolvePackageMirrors(context.Background(), cfg, "pypi", "demo-project", "demo-1.0.whl", digest)
	want := []string{mirror.URL + "/simple/demo-project/files/demo-1.0.whl"}
	if !slices.Equal(got, want) {
		t.Fatalf("resolvePackageMirrors() = %#v, want %#v", got, want)
	}
	if primaryHits.Load() != 1 || mirrorHits.Load() != 1 {
		t.Fatalf("index hits = primary:%d mirror:%d, want one request each", primaryHits.Load(), mirrorHits.Load())
	}
}

func TestResolvePackageMirrorsRejectsUntrustedOrMismatchedLinks(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	index := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResolverResponse(t, w, `<a href="https://cdn.example.test/demo-1.0.whl#sha256=ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff">wrong hash</a>`+
			`<a href="https://cdn.example.test/demo-1.0.whl.evil#sha256=`+digest+`">wrong name</a>`+
			`<a href="https://user:secret@cdn.example.test/demo-1.0.whl#sha256=`+digest+`">userinfo</a>`+
			`<a href="javascript:alert(1)#sha256=`+digest+`">bad scheme</a>`+
			`<a href="https://cdn.example.test/demo-1.0.whl">missing hash</a>`)
	}))
	defer index.Close()

	service := &Service{client: http.DefaultClient}
	got := service.resolvePackageMirrors(context.Background(), config.UpstreamConfig{Registry: index.URL}, "pypi", "demo", "demo-1.0.whl", digest)
	if len(got) != 0 {
		t.Fatalf("resolvePackageMirrors() = %#v, want no untrusted artifacts", got)
	}
}

func TestResolvePackageMirrorsRequiresTrustedIdentityAndBoundsIndexes(t *testing.T) {
	var hits atomic.Int32
	index := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeResolverResponse(t, w, `<a href="https://cdn.example.test/demo-1.0.whl#sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef">package</a>`)
	}))
	defer index.Close()

	service := &Service{client: http.DefaultClient}
	cfg := config.UpstreamConfig{Registry: index.URL}
	for i := range 10 {
		cfg.Mirrors = append(cfg.Mirrors, index.URL+"/mirror-"+url.PathEscape(strings.Repeat("x", i+1)))
	}
	if got := service.resolvePackageMirrors(context.Background(), cfg, "pypi", "demo", "demo-1.0.whl", ""); len(got) != 0 {
		t.Fatalf("resolvePackageMirrors() with missing digest = %#v, want none", got)
	}
	if hits.Load() != 0 {
		t.Fatalf("requests with missing digest = %d, want 0", hits.Load())
	}

	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	got := service.resolvePackageMirrors(context.Background(), cfg, "pypi", "demo", "demo-1.0.whl", digest)
	if len(got) != 1 {
		t.Fatalf("resolvePackageMirrors() candidates = %#v, want one deduplicated candidate", got)
	}
	if hits.Load() != 8 {
		t.Fatalf("index requests = %d, want the configured endpoint cap of 8", hits.Load())
	}
}

func TestResolvePackageMirrorsRejectsInvalidConfiguredIndexes(t *testing.T) {
	service := &Service{client: http.DefaultClient}
	cfg := config.UpstreamConfig{
		Registry: "file:///etc/passwd",
		Mirrors:  []string{"https://user:secret@example.test/simple"},
	}
	got := service.resolvePackageMirrors(context.Background(), cfg, "pypi", "demo", "demo-1.0.whl", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if len(got) != 0 {
		t.Fatalf("resolvePackageMirrors() = %#v, want no candidates from invalid indexes", got)
	}
}

func TestResolvePackageMirrorsStopsWhenContextIsCanceled(t *testing.T) {
	var hits atomic.Int32
	index := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeResolverResponse(t, w, `<html></html>`)
	}))
	defer index.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := &Service{client: http.DefaultClient}
	got := service.resolvePackageMirrors(ctx, config.UpstreamConfig{Registry: index.URL}, "pypi", "demo", "demo-1.0.whl", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if len(got) != 0 || hits.Load() != 0 {
		t.Fatalf("resolvePackageMirrors() with canceled context = %#v and %d requests, want none", got, hits.Load())
	}
}

func mirrorResolverPrimaryIndex(t *testing.T, hits *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/simple/demo-project/" {
			t.Errorf("primary path = %q, want /simple/demo-project/", r.URL.Path)
		}
		gotUser, gotPassword, ok := r.BasicAuth()
		if !ok || gotUser != "index-user" || gotPassword != "index-password" {
			t.Errorf("primary index auth = (%q, %q, %v), want configured basic auth", gotUser, gotPassword, ok)
		}
		writeResolverResponse(t, w, `<a href="https://cdn.example.test/files/demo-1.0.whl#sha256=ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff">wrong hash</a>`)
	}
}

func mirrorResolverMatchingIndex(t *testing.T, hits *atomic.Int32, digest string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/simple/demo-project/" {
			t.Errorf("mirror path = %q, want /simple/demo-project/", r.URL.Path)
		}
		writeResolverResponse(t, w, `<html><a href="files/demo-1.0.whl#sha256=`+strings.ToUpper(digest)+`">first</a><a href="files/demo-1.0.whl#sha256=`+digest+`">duplicate</a></html>`)
	}
}

func writeResolverResponse(t *testing.T, w io.Writer, body string) {
	t.Helper()
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("write test response: %v", err)
	}
}
