package container_test

import (
	"net/http"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
)

func TestRegistryEndpointManifestGetUsesBodyMediaTypeForImageManifestBlobFill(t *testing.T) {
	configDigest := endpointTestDigest("c")
	layerDigest := endpointTestDigest("1")
	manifests := endpointManifestService{
		manifest: cachedEndpointManifest(
			endpointTestDigest("m"),
			"",
			endpointImageManifestBody(t, configDigest, layerDigest),
		),
	}
	blobs := newEndpointBlobService()
	endpoint := newEndpointWithIOWorkers(t, &manifests, blobs)
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}

	blobs.release()
	blobs.waitRequests(t, 2)
	blobs.waitClosed(t, 2)
	assertEndpointBlobRequests(t, blobs.requestSnapshot(), []string{configDigest, layerDigest})
	assertEndpointBlobReadersClosed(t, blobs.closedSnapshot(), []string{configDigest, layerDigest})
}

func TestRegistryEndpointIndexManifestUsesBodyMediaTypeForChildBlobFill(t *testing.T) {
	childDigest := endpointTestDigest("a")
	configDigest := endpointTestDigest("c")
	layerDigest := endpointTestDigest("1")
	manifests := endpointManifestService{
		manifests: map[string]*cache.CachedManifest{
			"latest": cachedEndpointManifest(
				endpointTestDigest("m"),
				"",
				endpointIndexManifestBody(t, endpointIndexChild{digest: childDigest, platform: defaultEndpointPlatform()}),
			),
			childDigest: cachedEndpointManifest(
				childDigest,
				"",
				endpointImageManifestBody(t, configDigest, layerDigest),
			),
		},
	}
	blobs := newEndpointBlobService()
	endpoint := newEndpointWithIOWorkers(t, &manifests, blobs)
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	blobs.release()
	blobs.waitRequests(t, 2)
	blobs.waitClosed(t, 2)
	assertEndpointBlobRequests(t, blobs.requestSnapshot(), []string{configDigest, layerDigest})
	assertEndpointManifestRequests(t, manifests.requestSnapshot(), []string{"latest", childDigest})
}
