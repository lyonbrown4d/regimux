package pypi

import (
	"net/url"
	"strings"
	"testing"
)

func TestPackageIdentityNormalizesPipPEP658WheelURL(t *testing.T) {
	const digest = "4721f391ed90541fddacab5acf947aa0d3dc7d27b2e1e8eda2be8970586c3274"
	tail := "packages/https/files.pythonhosted.org/packages/six-1.17.0-py2.py3-none-any.whl"
	route, err := ParseTail("default", tail)
	if err != nil {
		t.Fatal(err)
	}
	route.Query = "token=signed-value&" + url.Values{
		packageProjectQuery: {"six"},
		packageHashQuery:    {digest + ".metadata"},
	}.Encode()

	req, route := packageIdentity(Request{}, route)

	if req.Project != "six" || req.SHA256 != digest {
		t.Fatalf("identity = (%q, %q), want project six and wheel SHA-256 %q", req.Project, req.SHA256, digest)
	}
	reference, _, _ := strings.Cut(route.Reference, "?")
	if !strings.HasSuffix(route.PackageTail, ".whl.metadata") || !strings.HasSuffix(route.UpstreamTail, ".whl.metadata") ||
		!strings.HasSuffix(route.DirectURL, ".whl.metadata") || !strings.HasSuffix(reference, ".whl.metadata") {
		t.Fatalf("pip metadata URL was not normalized to the metadata artifact: %#v", route)
	}
	if route.Query != "token=signed-value" {
		t.Fatalf("opaque signed query was not preserved: %q", route.Query)
	}
	if !packageIsMetadata(route) {
		t.Fatal("normalized route was not recognized as PEP 658 metadata")
	}
}

func TestPackageIdentityDoesNotNormalizeNonIdentityMetadataSuffix(t *testing.T) {
	const invalidDigest = "not-a-sha256.metadata"
	route, err := ParseTail("default", "packages/https/files.pythonhosted.org/packages/six-1.17.0-py2.py3-none-any.whl")
	if err != nil {
		t.Fatal(err)
	}
	route.Query = "token=signed-value.metadata&" + url.Values{
		packageProjectQuery: {"six"},
		packageHashQuery:    {invalidDigest},
	}.Encode()

	_, got := packageIdentity(Request{}, route)

	if strings.HasSuffix(got.PackageTail, ".whl.metadata") || strings.HasSuffix(got.DirectURL, ".whl.metadata") {
		t.Fatalf("invalid identity suffix changed the requested artifact: %#v", got)
	}
	if got.Query != "token=signed-value.metadata" {
		t.Fatalf("opaque query was changed: %q", got.Query)
	}

	validSuffixQuery := url.Values{
		packageProjectQuery: {"six"},
		packageHashQuery:    {strings.Repeat("a", 64) + ".metadata"},
	}.Encode() + "&token=signed-value"
	route, err = ParseTail("default", "packages/https/files.pythonhosted.org/packages/six-1.17.0-py2.py3-none-any.whl")
	if err != nil {
		t.Fatal(err)
	}
	route.Query = validSuffixQuery
	_, got = packageIdentity(Request{}, route)
	if strings.HasSuffix(got.PackageTail, ".whl.metadata") {
		t.Fatalf("non-final internal-looking query value changed the artifact: %#v", got)
	}
}
