package container_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestRegistryEndpointCachedManifestDoesNotRepeatBlobPrefill(t *testing.T) {
	manifest := cachedEndpointManifest(
		endpointTestDigest("a"),
		distribution.MediaTypeOCIManifest,
		endpointImageManifestBody(t, endpointTestDigest("c"), endpointTestDigest("1")),
	)
	manifest.Cache = cache.CacheHit
	manifests := endpointManifestService{manifest: manifest}
	blobs := newEndpointBlobService()
	endpoint := newEndpointWithIOWorkers(t, &manifests, blobs)
	t.Cleanup(blobs.release)
	baseURL := startAPIServer(t, endpoint)

	response := httpGet(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	body := readHTTPResponse(t, response)
	if response.StatusCode != http.StatusOK || !bytes.Equal(body, manifest.Body) {
		t.Fatalf("cached manifest status=%d body=%q", response.StatusCode, body)
	}
	blobs.assertNoRequests(t)
}
