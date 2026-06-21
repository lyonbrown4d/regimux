package meta_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestSQLStoreManifestHeadersAreAllowlisted(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)

	manifest, err := store.UpsertManifest(ctx, meta.ManifestRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Digest:     testDigest,
		MediaType:  distribution.MediaTypeDockerManifest,
		Size:       2,
		Headers: map[string][]string{
			"content-type":                                  {distribution.MediaTypeDockerManifest},
			distribution.HeaderContentLength:                {"2"},
			distribution.HeaderDockerContentDigest:          {testDigest},
			"etag":                                          {`"manifest"`},
			distribution.HeaderAuthorization:                {"Bearer secret-token"},
			distribution.HeaderWWWAuthenticate:              {"Bearer realm=secret"},
			"Set-Cookie":                                    {"session=secret"},
			"X-Upstream-Debug":                              {"debug-secret"},
			"X-Registry-Supports-Signatures":                {"1"},
			distribution.HeaderDockerDistributionAPIVersion: {"registry/2.0"},
		},
	})
	requireNoError(t, "upsert manifest", err)
	assertManifestHeadersAllowlisted(t, manifest.Headers)

	got, ok, err := store.Manifest(ctx, meta.ManifestKey{
		Alias:      "hub",
		Repository: "library/nginx",
		Digest:     testDigest,
	})
	requireNoError(t, "get manifest", err)
	if !ok {
		t.Fatal("expected manifest lookup")
	}
	assertManifestHeadersAllowlisted(t, got.Headers)
}

func assertManifestHeadersAllowlisted(t *testing.T, headers map[string][]string) {
	t.Helper()

	if len(headers) != 4 {
		t.Fatalf("manifest headers len = %d, want 4: %#v", len(headers), headers)
	}
	header := http.Header(headers)
	if got := header.Get(distribution.HeaderContentType); got != distribution.MediaTypeDockerManifest {
		t.Fatalf("content-type = %q", got)
	}
	if got := header.Get(distribution.HeaderContentLength); got != "2" {
		t.Fatalf("content-length = %q", got)
	}
	if got := header.Get(distribution.HeaderDockerContentDigest); got != testDigest {
		t.Fatalf("docker-content-digest = %q", got)
	}
	if got := header.Get(distribution.HeaderETag); got != `"manifest"` {
		t.Fatalf("etag = %q", got)
	}
	for _, key := range []string{
		distribution.HeaderAuthorization,
		distribution.HeaderWWWAuthenticate,
		distribution.HeaderDockerDistributionAPIVersion,
		"Set-Cookie",
		"X-Upstream-Debug",
		"X-Registry-Supports-Signatures",
	} {
		if got := header.Get(key); got != "" {
			t.Fatalf("%s was persisted: %q", key, got)
		}
	}
}
