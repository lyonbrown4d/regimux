package upstream_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestPublicMirrorForbiddenWithoutAllowlistEvidenceRemainsTerminal(t *testing.T) {
	var mirrorRequests atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorRequests.Add(1)
		w.WriteHeader(http.StatusForbidden)
		writeString(t, w, `{"errors":[{"code":"DENIED","message":"requested access to the resource is denied"}]}`)
	}))
	defer mirror.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(healthyManifestHandler(t, &primaryRequests))
	defer primary.Close()
	client := newTestClient(map[string]upstream.Config{
		"hub": {Registry: primary.URL, Mirrors: []string{mirror.URL}, MirrorForbiddenPolicy: "public"},
	})

	_, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub", Repo: "library/nginx", Reference: "latest",
	})
	requireErrorStatus(t, err, http.StatusForbidden)
	requireEqual(t, mirrorRequests.Load(), int32(1), "mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(0), "primary requests")
}

func TestPublicMirrorAllowlistChallengeFailoverPreservesManifestAndCoversTagsAndBlob(t *testing.T) {
	const (
		token  = "anonymous-pull-token"
		digest = "sha256:0123456789abcdef"
	)
	manifestBytes := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:child","size":123}]}`
	var tokenRequests atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests.Add(1)
		writeString(t, w, `{"token":"`+token+`"}`)
	}))
	defer tokenServer.Close()

	var mirrorRequests atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorRequests.Add(1)
		if r.Header.Get(distribution.HeaderAuthorization) == "" {
			w.Header().Set(distribution.HeaderWWWAuthenticate, fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, tokenServer.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get(distribution.HeaderAuthorization); got != "Bearer "+token {
			t.Errorf("mirror authorization = %q", got)
		}
		w.WriteHeader(http.StatusForbidden)
		writeString(t, w, `{"errors":[{"code":"DENIED","message":"this image is not in the allowlist"}]}`)
	}))
	defer mirror.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryRequests.Add(1)
		if got := r.Header.Get(distribution.HeaderAuthorization); got != "" {
			t.Errorf("primary received leaked authorization %q", got)
		}
		switch {
		case strings.Contains(r.URL.Path, "/manifests/"):
			w.Header().Set(distribution.HeaderDockerContentDigest, digest)
			w.Header().Set(distribution.HeaderContentType, "application/vnd.oci.image.index.v1+json")
			w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(manifestBytes)))
			writeString(t, w, manifestBytes)
		case strings.HasSuffix(r.URL.Path, "/tags/list"):
			writeString(t, w, `{"name":"library/nginx","tags":["latest"]}`)
		case strings.Contains(r.URL.Path, "/blobs/"):
			w.Header().Set(distribution.HeaderDockerContentDigest, digest)
			writeString(t, w, "blob")
		default:
			t.Errorf("unexpected primary path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer primary.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {Registry: primary.URL, Mirrors: []string{mirror.URL}, MirrorForbiddenPolicy: "public", MirrorPolicy: "ordered"},
	})
	manifest, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub", Repo: "library/nginx", Reference: digest,
	})
	requireNoError(t, err, "GetManifest by digest")
	body := readAndCloseBody(t, manifest.Body)
	requireEqual(t, string(body), manifestBytes, "multiarch manifest bytes")
	requireEqual(t, manifest.Digest, digest, "manifest digest")

	tags, err := client.ListTags(context.Background(), upstream.ListTagsRequest{UpstreamAlias: "hub", Repo: "library/redis"})
	requireNoError(t, err, "ListTags")
	closeBody(t, tags.Body)

	blob, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{UpstreamAlias: "hub", Repo: "library/busybox", Digest: digest})
	requireNoError(t, err, "GetBlob")
	blobBody := readAndCloseBody(t, blob.Body)
	requireEqual(t, string(blobBody), "blob", "blob bytes")

	requireEqual(t, primaryRequests.Load(), int32(3), "primary requests")
	requireEqual(t, mirrorRequests.Load(), int32(6), "mirror challenge/retry requests")
	if tokenRequests.Load() > 3 {
		t.Fatalf("token requests = %d, want bounded challenge-token reuse", tokenRequests.Load())
	}
}

func readAndCloseBody(t *testing.T, body io.ReadCloser) []byte {
	t.Helper()
	data, err := io.ReadAll(body)
	closeBody(t, body)
	requireNoError(t, err, "read response body")
	return data
}

func TestPublicMirrorAllowlistFailoverStopsAfterConfiguredEndpoints(t *testing.T) {
	var mirrorRequests atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorRequests.Add(1)
		w.WriteHeader(http.StatusForbidden)
		writeString(t, w, `denied: this image is not in the allowlist`)
	}))
	defer mirror.Close()
	var primaryRequests atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryRequests.Add(1)
		w.WriteHeader(http.StatusForbidden)
		writeString(t, w, `denied: this image is not in the allowlist`)
	}))
	defer primary.Close()
	client := newTestClient(map[string]upstream.Config{
		"hub": {Registry: primary.URL, Mirrors: []string{mirror.URL}, MirrorForbiddenPolicy: "public", MirrorPolicy: "ordered"},
	})

	_, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub", Repo: "library/nginx", Reference: "latest",
	})
	requireErrorStatus(t, err, http.StatusForbidden)
	requireEqual(t, mirrorRequests.Load(), int32(1), "mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(1), "primary requests")
}
