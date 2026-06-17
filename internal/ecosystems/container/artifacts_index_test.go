package container_test

import (
	"net/http"
	"runtime"
	"slices"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestRegistryEndpointIndexManifestFillsCurrentPlatformChildBlobsAsync(t *testing.T) {
	childDigest := endpointTestDigest("child")
	configDigest := endpointTestDigest("c")
	layerDigest := endpointTestDigest("1")
	manifests := endpointManifestService{
		manifests: map[string]*cache.CachedManifest{
			"latest": cachedEndpointManifest(
				endpointTestDigest("m"),
				distribution.MediaTypeOCIIndex,
				endpointIndexManifestBody(t, childDigest, currentEndpointPlatform()),
			),
			childDigest: cachedEndpointManifest(
				childDigest,
				distribution.MediaTypeOCIManifest,
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

func TestRegistryEndpointIndexManifestSkipsOtherPlatform(t *testing.T) {
	manifests := endpointManifestService{
		manifests: map[string]*cache.CachedManifest{
			"latest": cachedEndpointManifest(
				endpointTestDigest("m"),
				distribution.MediaTypeOCIIndex,
				endpointIndexManifestBody(t, endpointTestDigest("child"), otherEndpointPlatform()),
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
	blobs.assertNoRequests(t)
}

func endpointIndexManifestBody(t *testing.T, childDigest string, platform *ocispec.Platform) []byte {
	t.Helper()
	return marshalEndpointManifest(t, ocispec.Index{
		MediaType: distribution.MediaTypeOCIIndex,
		Manifests: []ocispec.Descriptor{{
			MediaType: distribution.MediaTypeOCIManifest,
			Digest:    digest.Digest(childDigest),
			Size:      512,
			Platform:  platform,
		}},
	})
}

func currentEndpointPlatform() *ocispec.Platform {
	return &ocispec.Platform{OS: runtime.GOOS, Architecture: runtime.GOARCH}
}

func otherEndpointPlatform() *ocispec.Platform {
	if runtime.GOARCH == "amd64" {
		return &ocispec.Platform{OS: runtime.GOOS, Architecture: "arm64"}
	}
	return &ocispec.Platform{OS: runtime.GOOS, Architecture: "amd64"}
}

func assertEndpointManifestRequests(t *testing.T, requests []cache.ManifestRequest, want []string) {
	t.Helper()
	if len(requests) < len(want) {
		t.Fatalf("manifest requests = %d, want at least %d: %#v", len(requests), len(want), requests)
	}
	for i := range want {
		if !slices.ContainsFunc(requests, func(req cache.ManifestRequest) bool {
			return req.UpstreamAlias == "hub" &&
				req.Repo == "library/alpine" &&
				req.Reference == want[i] &&
				req.Method == http.MethodGet
		}) {
			t.Fatalf("missing manifest GET for reference %s in %#v", want[i], requests)
		}
	}
}
