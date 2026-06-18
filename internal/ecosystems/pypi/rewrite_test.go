package pypi_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
)

func TestRewriteSimpleIndexLinksRewritesHTMLHrefAttributes(t *testing.T) {
	const (
		alias       = "mirror"
		upstreamURL = "https://pypi.example.org/simple/demo/"
	)
	wheelHref := expectedLocalHref(t, alias, upstreamURL, "/packages/demo-1.0.0-py3-none-any.whl") + "#sha256=abc"
	sdistHref := expectedLocalHref(t, alias, upstreamURL, "/packages/demo-1.0.0.tar.gz")
	relativeHref := expectedLocalHref(t, alias, upstreamURL, "/packages/demo-1.0.0.zip") + "?download=1#sha256=def"

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "double quoted href",
			body: `<a data-requires-python="&gt;=3.8" href="https://pypi.example.org/packages/demo-1.0.0-py3-none-any.whl#sha256=abc">wheel</a>`,
			want: `<a data-requires-python="&gt;=3.8" href="` + wheelHref + `">wheel</a>`,
		},
		{
			name: "single quoted href",
			body: `<a href='/packages/demo-1.0.0.tar.gz'>sdist</a>`,
			want: `<a href='` + sdistHref + `'>sdist</a>`,
		},
		{
			name: "relative href",
			body: `<a HREF="../../packages/demo-1.0.0.zip?download=1#sha256=def">zip</a>`,
			want: `<a HREF="` + relativeHref + `">zip</a>`,
		},
		{
			name: "non href attributes",
			body: `<a data-href="/packages/demo-1.0.0.whl" title="href='/packages/demo-1.0.0.whl'">metadata</a>`,
			want: `<a data-href="/packages/demo-1.0.0.whl" title="href='/packages/demo-1.0.0.whl'">metadata</a>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(pypi.RewriteSimpleIndexLinks([]byte(tt.body), alias, upstreamURL))
			if got != tt.want {
				t.Fatalf("rewritten body = %q, want %q", got, tt.want)
			}
		})
	}
}
