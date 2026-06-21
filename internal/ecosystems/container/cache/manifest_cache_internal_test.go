package cache

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestManifestCacheEnvelopeHeadersAreAllowlisted(t *testing.T) {
	body := []byte(`{"schemaVersion":2}`)
	record := meta.ManifestRecord{
		Digest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MediaType: distribution.MediaTypeDockerManifest,
		Size:      int64(len(body)),
		Headers: map[string][]string{
			"content-type":                                  {distribution.MediaTypeDockerManifest},
			distribution.HeaderContentLength:                {"19"},
			distribution.HeaderDockerContentDigest:          {"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			"etag":                                          {`"manifest"`},
			distribution.HeaderAuthorization:                {"Bearer secret-token"},
			distribution.HeaderWWWAuthenticate:              {"Bearer realm=secret"},
			"Set-Cookie":                                    {"session=secret"},
			"X-Upstream-Debug":                              {"debug-secret"},
			distribution.HeaderDockerDistributionAPIVersion: {"registry/2.0"},
		},
	}

	data, err := manifestEnvelopeFromRecord(record, body)
	if err != nil {
		t.Fatalf("manifest envelope: %v", err)
	}
	for _, value := range [][]byte{
		[]byte("secret-token"),
		[]byte("Bearer realm=secret"),
		[]byte("session=secret"),
		[]byte("debug-secret"),
		[]byte("registry/2.0"),
	} {
		if bytes.Contains(data, value) {
			t.Fatalf("manifest envelope persisted forbidden header value %q in %s", value, data)
		}
	}

	cached, err := manifestFromEnvelope(data)
	if err != nil {
		t.Fatalf("manifest from envelope: %v", err)
	}
	assertEnvelopeHeadersAllowlisted(t, cached.Headers)
}

func assertEnvelopeHeadersAllowlisted(t *testing.T, headers http.Header) {
	t.Helper()

	if len(headers) != 4 {
		t.Fatalf("manifest headers len = %d, want 4: %#v", len(headers), headers)
	}
	if got := headers.Get(distribution.HeaderContentType); got != distribution.MediaTypeDockerManifest {
		t.Fatalf("content-type = %q", got)
	}
	if got := headers.Get(distribution.HeaderContentLength); got != "19" {
		t.Fatalf("content-length = %q", got)
	}
	if got := headers.Get(distribution.HeaderDockerContentDigest); got != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("docker-content-digest = %q", got)
	}
	if got := headers.Get(distribution.HeaderETag); got != `"manifest"` {
		t.Fatalf("etag = %q", got)
	}
	for _, key := range []string{
		distribution.HeaderAuthorization,
		distribution.HeaderWWWAuthenticate,
		distribution.HeaderDockerDistributionAPIVersion,
		"Set-Cookie",
		"X-Upstream-Debug",
	} {
		if got := headers.Get(key); got != "" {
			t.Fatalf("%s was persisted: %q", key, got)
		}
	}
}
