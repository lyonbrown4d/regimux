package pypi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestLegacyPackageProjectDerivationIsConservative(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{name: "wheel distribution before first dash", filename: "My_Project-1.0-py3-none-any.whl", want: "my-project"},
		{name: "hyphenated source distribution", filename: "my-project-2.4.1.tar.gz", want: "my-project"},
		{name: "hyphenated source distribution with release suffix", filename: "my-project-2.4-rc1.zip", want: "my-project"},
		{name: "unknown format", filename: "my-project-2.4.1.exe"},
		{name: "source without numeric version", filename: "my-project-latest.tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := legacyPackageProject(tt.filename)
			if got != tt.want {
				t.Fatalf("legacyPackageProject(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestLegacyPackageIdentityFindsExactFreshTrustedIndexLink(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var indexHits atomic.Int32
	index := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		indexHits.Add(1)
		if r.URL.Path != "/simple/demo/" {
			t.Errorf("simple index path = %q, want /simple/demo/", r.URL.Path)
		}
		writeLegacyIdentityResponse(t, w, `<a href="https://other.example.test/demo-1.0-py3-none-any.whl?token=abc#sha256=`+digest+`">same name, other host</a>`+
			`<a href="https://files.example.test/demo-1.0-py3-none-any.whl?token=wrong#sha256=`+digest+`">same host, other query</a>`+
			`<a href="https://files.example.test/demo-1.0-py3-none-any.whl?token=abc#sha256=`+strings.ToUpper(digest)+`">exact artifact</a>`)
	}))
	defer index.Close()

	service := &Service{client: http.DefaultClient, metadataSlots: make(chan struct{}, metadataTransformConcurrency)}
	cfg := config.UpstreamConfig{Registry: index.URL}
	route := directPackageRoute(t, "https://files.example.test/demo-1.0-py3-none-any.whl", "token=abc&__regimux_trace=proxy")
	project, gotDigest := service.legacyPackageIdentity(context.Background(), cfg, route)
	if project != "demo" || gotDigest != digest {
		t.Fatalf("legacyPackageIdentity() = (%q, %q), want (%q, %q)", project, gotDigest, "demo", digest)
	}
	if indexHits.Load() != 1 {
		t.Fatalf("simple index requests = %d, want 1", indexHits.Load())
	}
}

func TestLegacyPackageIdentityRejectsSameFilenameFromDifferentURLOrMissingHash(t *testing.T) {
	index := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeLegacyIdentityResponse(t, w, `<a href="https://other.example.test/demo-1.0-py3-none-any.whl#sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef">wrong URL</a>`+
			`<a href="https://files.example.test/demo-1.0-py3-none-any.whl">missing identity</a>`)
	}))
	defer index.Close()

	service := &Service{client: http.DefaultClient, metadataSlots: make(chan struct{}, metadataTransformConcurrency)}
	route := directPackageRoute(t, "https://files.example.test/demo-1.0-py3-none-any.whl", "")
	project, digest := service.legacyPackageIdentity(context.Background(), config.UpstreamConfig{Registry: index.URL}, route)
	if project != "" || digest != "" {
		t.Fatalf("legacyPackageIdentity() = (%q, %q), want no identity", project, digest)
	}
}

func TestLegacyPackageIdentityUsesCachedRewrittenIndex(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var indexHits atomic.Int32
	index := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		indexHits.Add(1)
		writeLegacyIdentityResponse(t, w, `<a href="https://files.example.test/demo-1.0-py3-none-any.whl?token=abc#sha256=`+digest+`">wheel</a>`)
	}))
	defer index.Close()

	ctx := context.Background()
	service := legacyIdentityServiceWithCache(ctx, t, index.URL)
	resp, err := service.Get(ctx, Request{Alias: "pypi", Tail: "simple/demo/"})
	if err != nil {
		t.Fatalf("fetch simple index: %v", err)
	}
	if resp.Body != nil {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close simple response: %v", err)
		}
	}

	route := directPackageRoute(t, "https://files.example.test/demo-1.0-py3-none-any.whl", "token=abc&__regimux_trace=proxy")
	project, gotDigest := service.legacyPackageIdentity(ctx, config.UpstreamConfig{Registry: index.URL}, route)
	if project != "demo" || gotDigest != digest {
		t.Fatalf("legacyPackageIdentity() = (%q, %q), want cached identity (%q, %q)", project, gotDigest, "demo", digest)
	}
	if indexHits.Load() != 1 {
		t.Fatalf("simple index requests = %d, want cached lookup to avoid a refetch", indexHits.Load())
	}
}

func directPackageRoute(t *testing.T, rawURL, query string) Route {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse package URL: %v", err)
	}
	route, err := ParseTail("pypi", "packages/"+parsed.Scheme+"/"+parsed.Host+"/"+strings.TrimLeft(parsed.EscapedPath(), "/"))
	if err != nil {
		t.Fatalf("parse package route: %v", err)
	}
	route.Query = query
	return route
}

func legacyIdentityServiceWithCache(ctx context.Context, t *testing.T, upstreamURL string) *Service {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "regimux.db")
	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: databasePath})
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Errorf("close metadata: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open objects: %v", err)
	}
	return NewService(ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstreamURL}}},
		Metadata: metadata,
		Objects:  objects,
	})
}

func writeLegacyIdentityResponse(t *testing.T, w io.Writer, body string) {
	t.Helper()
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("write test response: %v", err)
	}
}
