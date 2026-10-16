package pypi_test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
)

func TestSimpleIndexRootRelativeWheelLinkCanBeFollowed(t *testing.T) {
	const wheel = "wheel-bytes-from-basepath-registry"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(wheel)))
	upstream := newPyPIFollowLinkServer(t, digest, wheel)
	service := newTestService(t.Context(), t, upstream.URL+"/pypi", nil)

	index, err := service.Get(t.Context(), pypi.Request{Alias: "pypi", Tail: "simple/concurrency-fixture", SkipPullRecord: true})
	requireNoError(t, "get simple index", err)
	href := firstHref(t, readResponse(t, index))
	parsed, err := url.Parse(href)
	requireNoError(t, "parse rewritten href", err)
	assertRewrittenWheelURL(t, parsed, digest)
	assertFollowedWheelBody(t, service, parsed, wheel)
}

func newPyPIFollowLinkServer(t *testing.T, digest, wheel string) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pypi/simple/concurrency-fixture/":
			w.Header().Set("Content-Type", "text/html")
			writeResponse(t, w, fmt.Sprintf(`<a href="/pypi/files/concurrency_fixture-1.0.0-py3-none-any.whl?signature=kept#sha256=%s">wheel</a>`, digest))
		case "/pypi/files/concurrency_fixture-1.0.0-py3-none-any.whl":
			if r.URL.Query().Get("signature") != "kept" {
				t.Errorf("wheel query = %q, want signature=kept", r.URL.RawQuery)
			}
			writeResponse(t, w, wheel)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func firstHref(t *testing.T, index string) string {
	t.Helper()
	const prefix = `href="`
	start := strings.Index(index, prefix)
	if start < 0 {
		t.Fatalf("rewritten simple index has no href: %s", index)
	}
	start += len(prefix)
	end := strings.IndexByte(index[start:], '"')
	if end < 0 {
		t.Fatalf("rewritten simple index has unterminated href: %s", index)
	}
	return index[start : start+end]
}

func assertRewrittenWheelURL(t *testing.T, parsed *url.URL, digest string) {
	t.Helper()
	if !strings.HasPrefix(parsed.Path, "/pypi/pypi/packages/http/") || !strings.HasSuffix(parsed.Path, "/pypi/files/concurrency_fixture-1.0.0-py3-none-any.whl") {
		t.Fatalf("rewritten href path = %q", parsed.Path)
	}
	if parsed.Query().Get("signature") != "kept" || parsed.Query().Get("__regimux_sha256") != digest {
		t.Fatalf("rewritten href query lost source metadata: %q", parsed.RawQuery)
	}
	if parsed.Fragment != "sha256="+digest {
		t.Fatalf("rewritten href fragment = %q, want sha256=%s", parsed.Fragment, digest)
	}
}

func assertFollowedWheelBody(t *testing.T, service *pypi.Service, parsed *url.URL, want string) {
	t.Helper()
	tail := strings.TrimPrefix(parsed.EscapedPath(), "/pypi/pypi/")
	fetched, err := service.Get(t.Context(), pypi.Request{Alias: "pypi", Tail: tail, Query: parsed.RawQuery, SkipPullRecord: true})
	requireNoError(t, "follow rewritten wheel link", err)
	if got := readResponse(t, fetched); got != want {
		t.Fatalf("wheel body = %q, want %q", got, want)
	}
}

func TestSimpleIndexHrefResolutionPreservesURLFormsAndRejectsUnsupportedSchemes(t *testing.T) {
	const upstreamURL = "https://files.example.test/pypi/simple/demo/"
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, testCase := range pypiHrefCases(hash) {
		t.Run(testCase.name, func(t *testing.T) {
			got := string(pypi.RewriteSimpleIndexLinks([]byte(`<a href="`+testCase.href+`">wheel</a>`), "mirror", upstreamURL))
			if !strings.Contains(got, `href="`+testCase.want+`"`) {
				t.Fatalf("rewritten href = %q, want href %q", got, testCase.want)
			}
		})
	}
}

type pypiHrefCase struct{ name, href, want string }

func pypiHrefCases(hash string) []pypiHrefCase {
	local := "/pypi/mirror/packages/https/files.example.test/pypi/files/demo.whl?download=1&__regimux_project=demo&__regimux_sha256=" + hash + "#sha256=" + hash
	return []pypiHrefCase{
		{name: "root relative base path", href: "/pypi/files/demo.whl?download=1#sha256=" + hash, want: local},
		{name: "path relative base path", href: "../../files/demo.whl?download=1#sha256=" + hash, want: local},
		{name: "absolute URL", href: "https://files.example.test/pypi/files/demo.whl?download=1#sha256=" + hash, want: local},
		{name: "unsupported scheme", href: "ftp://files.example.test/pypi/files/demo.whl", want: "ftp://files.example.test/pypi/files/demo.whl"},
	}
}
