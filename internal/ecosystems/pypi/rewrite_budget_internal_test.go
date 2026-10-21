package pypi

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestSimpleIndexOutputBudgetBoundsManyShortLinks(t *testing.T) {
	const upstream = "https://pypi.org/simple/pkg/"
	const publicURL = "https://proxy.example.test/a/long/base/path"
	body := strings.Repeat(`<a href="one.whl">one</a>`, 20)
	want := rewriteSimpleIndexLinks([]byte(body), "alias", upstream, publicURL)
	if len(want) <= len(body) {
		t.Fatal("fixture did not expand when links were rewritten")
	}

	got, err := rewriteSimpleIndexLinksLimited([]byte(body), "alias", upstream, publicURL, int64(len(want)))
	if err != nil {
		t.Fatalf("rewrite at exact output limit: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("bounded rewrite differs from public rewrite")
	}
	if _, err := rewriteSimpleIndexLinksLimited([]byte(body), "alias", upstream, publicURL, int64(len(want)-1)); !errors.Is(err, errSimpleIndexOutputLimit) {
		t.Fatalf("rewrite below required output size error = %v", err)
	}
}

func TestSimpleIndexOutputBudgetBoundsMultipleHrefsInOneTag(t *testing.T) {
	const upstream = "https://pypi.org/simple/pkg/"
	const publicURL = "https://proxy.example.test/a/long/base/path"
	body := []byte(`<a href="one.whl" href='two.whl'>package</a>`)
	want := rewriteSimpleIndexLinks(body, "alias", upstream, publicURL)
	if len(want) <= len(body) {
		t.Fatal("fixture did not expand when both hrefs were rewritten")
	}
	if _, err := rewriteSimpleIndexLinksLimited(body, "alias", upstream, publicURL, int64(len(want)-1)); !errors.Is(err, errSimpleIndexOutputLimit) {
		t.Fatalf("multiple hrefs below output budget error = %v", err)
	}
}
