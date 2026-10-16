package npm_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/npm"
)

type tarballURLCase struct {
	name string
	link func(registryURL, packageName, filename string) string
}

func TestMetadataTarballURLsResolveAgainstRegistryBaseAndCanBeFollowed(t *testing.T) {
	for index, testCase := range metadataTarballURLCases() {
		t.Run(testCase.name, func(t *testing.T) {
			runMetadataTarballURLCase(t, index, testCase)
		})
	}
}

func metadataTarballURLCases() []tarballURLCase {
	return []tarballURLCase{
		{
			name: "absolute registry URL with base path",
			link: func(registryURL, packageName, filename string) string {
				return registryURL + "/" + packageName + "/-/" + filename + "?signature=kept#download-fragment"
			},
		},
		{
			name: "root relative URL with base path",
			link: func(_ string, packageName, filename string) string {
				return "/npm/" + packageName + "/-/" + filename + "?signature=kept#download-fragment"
			},
		},
		{
			name: "path relative URL with base path",
			link: func(_ string, packageName, filename string) string {
				return packageName + "/-/" + filename + "?signature=kept#download-fragment"
			},
		},
		{
			name: "canonical registry absolute URL",
			link: func(_ string, packageName, filename string) string {
				return "https://registry.npmjs.org/" + packageName + "/-/" + filename + "?signature=kept#download-fragment"
			},
		},
	}
}

func runMetadataTarballURLCase(t *testing.T, index int, testCase tarballURLCase) {
	t.Helper()
	packageName := fmt.Sprintf("metadata-link-%d", index)
	filename := packageName + "-1.0.0.tgz"
	registryURL := ""
	metadataBody := func() string {
		link := testCase.link(registryURL, packageName, filename)
		return fmt.Sprintf(`{"name":%q,"versions":{"1.0.0":{"dist":{"tarball":%q}}}}`, packageName, link)
	}
	upstream := newNPMTarballServer(t, "/npm/"+packageName, "/npm/"+packageName+"/-/"+filename, metadataBody, "npm-tarball-bytes")
	registryURL = upstream.URL + "/npm"
	service := newTestService(t.Context(), t, registryURL, time.Minute)
	assertMetadataTarballLinkCanBeFollowed(t, service, packageName, filename, "/npm/npmjs/"+packageName+"/-/"+filename, "npm-tarball-bytes")
}

func newNPMTarballServer(t *testing.T, metadataPath, tarballPath string, metadataBody func() string, tarballBody string) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == metadataPath {
			w.Header().Set("Content-Type", "application/json")
			writeResponse(t, w, metadataBody())
			return
		}
		if r.URL.Path != tarballPath {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("signature") != "kept" {
			t.Errorf("tarball query = %q, want signature=kept", r.URL.RawQuery)
		}
		writeResponse(t, w, tarballBody)
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func assertMetadataTarballLinkCanBeFollowed(t *testing.T, service *npm.Service, packageName, filename, wantPath, wantBody string) {
	t.Helper()
	metadata, err := service.Get(t.Context(), npm.Request{Alias: "npmjs", Tail: packageName, SkipPullRecord: true})
	requireNoError(t, "get metadata", err)
	rewritten := metadataTarball(t, readJSON(t, metadata), "1.0.0")
	parsed, err := url.Parse(rewritten)
	requireNoError(t, "parse rewritten tarball", err)
	assertTarballURLParts(t, parsed, wantPath)
	tarballTail := strings.TrimPrefix(parsed.EscapedPath(), "/npm/npmjs/")
	tarball, err := service.Get(t.Context(), npm.Request{Alias: "npmjs", Tail: tarballTail, Query: parsed.RawQuery, SkipPullRecord: true})
	requireNoError(t, "follow rewritten tarball", err)
	if got := readBody(t, tarball); got != wantBody {
		t.Fatalf("tarball body = %q, want %q", got, wantBody)
	}
}

func assertTarballURLParts(t *testing.T, parsed *url.URL, wantPath string) {
	t.Helper()
	if !strings.HasSuffix(parsed.EscapedPath(), wantPath) {
		t.Fatalf("rewritten tarball path = %q, want suffix %q", parsed.EscapedPath(), wantPath)
	}
	if parsed.Query().Get("signature") != "kept" {
		t.Fatalf("rewritten tarball query = %q, want signature=kept", parsed.RawQuery)
	}
	if parsed.Fragment != "download-fragment" {
		t.Fatalf("rewritten tarball fragment = %q, want download-fragment", parsed.Fragment)
	}
}

func TestMetadataTarballURLDoesNotRewriteUnsupportedScheme(t *testing.T) {
	const original = "ftp://registry.npmjs.org/demo/-/demo-1.0.0.tgz"
	metadataBody := fmt.Sprintf(`{"name":"demo","versions":{"1.0.0":{"dist":{"tarball":%q}},"2.0.0":{"dist":{"tarball":%q}}}}`, original, original)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, metadataBody)
	}))
	t.Cleanup(upstream.Close)
	service := newTestService(t.Context(), t, upstream.URL, time.Minute)
	resp, err := service.Get(t.Context(), npm.Request{Alias: "npmjs", Tail: "demo", SkipPullRecord: true})
	requireNoError(t, "get metadata", err)
	doc := readJSON(t, resp)
	assertTarball(t, doc, "1.0.0", original)
	assertTarball(t, doc, "2.0.0", original)
}

func TestScopedMetadataTarballURLsFollowAcrossEncodedPackageRoutes(t *testing.T) {
	for _, tail := range []string{"@scope/pkg", "@scope%2fpkg"} {
		t.Run(tail, func(t *testing.T) {
			runScopedMetadataTarballCase(t, tail)
		})
	}
}

func runScopedMetadataTarballCase(t *testing.T, tail string) {
	t.Helper()
	const filename = "pkg-1.0.0.tgz"
	metadataBody := fmt.Sprintf(`{"name":"@scope/pkg","versions":{"1.0.0":{"dist":{"tarball":"/npm/@scope/pkg/-/%s?signature=kept"}}}}`, filename)
	upstream := newNPMTarballServer(t, "/npm/@scope/pkg", "/npm/@scope/pkg/-/"+filename, func() string { return metadataBody }, "scoped-npm-tarball-bytes")
	service := newTestService(t.Context(), t, upstream.URL+"/npm", time.Minute)
	assertMetadataTarballLinkCanBeFollowed(t, service, tail, filename, "/npm/npmjs/@scope/pkg/-/"+filename, "scoped-npm-tarball-bytes")
}

func metadataTarball(t *testing.T, doc map[string]any, version string) string {
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
	tarball, ok := dist["tarball"].(string)
	if !ok {
		t.Fatalf("tarball has type %T, want string", dist["tarball"])
	}
	return tarball
}
