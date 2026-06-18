package container_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/containerd/platforms"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestRegistryEndpointIndexManifestFillsDefaultPlatformChildBlobsAsync(t *testing.T) {
	childDigest := endpointTestDigest("child")
	configDigest := endpointTestDigest("c")
	layerDigest := endpointTestDigest("1")
	manifests := endpointManifestService{
		manifests: map[string]*cache.CachedManifest{
			"latest": cachedEndpointManifest(
				endpointTestDigest("m"),
				distribution.MediaTypeOCIIndex,
				endpointIndexManifestBody(t, endpointIndexChild{digest: childDigest, platform: defaultEndpointPlatform()}),
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

func TestRegistryEndpointIndexManifestSkipsUnconfiguredPlatform(t *testing.T) {
	manifests := endpointManifestService{
		manifests: map[string]*cache.CachedManifest{
			"latest": cachedEndpointManifest(
				endpointTestDigest("m"),
				distribution.MediaTypeOCIIndex,
				endpointIndexManifestBody(t, endpointIndexChild{digest: endpointTestDigest("child"), platform: nonDefaultEndpointPlatform()}),
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

func TestRegistryEndpointIndexManifestFillsConfiguredPlatformChildBlobsAsync(t *testing.T) {
	activateEndpointPrewarmPlatforms(t, []string{"linux/arm64", "linux/ppc64le"})
	amd64ChildDigest := endpointTestDigest("a")
	arm64ChildDigest := endpointTestDigest("b")
	configDigest := endpointTestDigest("d")
	layerDigest := endpointTestDigest("2")
	manifests := endpointManifestService{
		manifests: map[string]*cache.CachedManifest{
			"latest": cachedEndpointManifest(
				endpointTestDigest("m"),
				distribution.MediaTypeOCIIndex,
				endpointIndexManifestBody(t,
					endpointIndexChild{digest: amd64ChildDigest, platform: linuxAMD64EndpointPlatform()},
					endpointIndexChild{digest: arm64ChildDigest, platform: linuxARM64EndpointPlatform()},
				),
			),
			amd64ChildDigest: cachedEndpointManifest(
				amd64ChildDigest,
				distribution.MediaTypeOCIManifest,
				endpointImageManifestBody(t, endpointTestDigest("e"), endpointTestDigest("3")),
			),
			arm64ChildDigest: cachedEndpointManifest(
				arm64ChildDigest,
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
	assertEndpointManifestRequests(t, manifests.requestSnapshot(), []string{"latest", arm64ChildDigest})
}

func TestRegistryEndpointIndexManifestPrewarmAllPlatformsFillsEveryChildBlobsAsync(t *testing.T) {
	activateEndpointPrewarmPlatforms(t, []string{"all"})
	amd64ChildDigest := endpointTestDigest("a")
	arm64ChildDigest := endpointTestDigest("b")
	amd64ConfigDigest := endpointTestDigest("c")
	amd64LayerDigest := endpointTestDigest("1")
	arm64ConfigDigest := endpointTestDigest("d")
	arm64LayerDigest := endpointTestDigest("2")
	manifests := endpointManifestService{
		manifests: map[string]*cache.CachedManifest{
			"latest": cachedEndpointManifest(
				endpointTestDigest("m"),
				distribution.MediaTypeOCIIndex,
				endpointIndexManifestBody(t,
					endpointIndexChild{digest: amd64ChildDigest, platform: linuxAMD64EndpointPlatform()},
					endpointIndexChild{digest: arm64ChildDigest, platform: linuxARM64EndpointPlatform()},
				),
			),
			amd64ChildDigest: cachedEndpointManifest(
				amd64ChildDigest,
				distribution.MediaTypeOCIManifest,
				endpointImageManifestBody(t, amd64ConfigDigest, amd64LayerDigest),
			),
			arm64ChildDigest: cachedEndpointManifest(
				arm64ChildDigest,
				distribution.MediaTypeOCIManifest,
				endpointImageManifestBody(t, arm64ConfigDigest, arm64LayerDigest),
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
	blobs.waitRequests(t, 4)
	blobs.waitClosed(t, 4)
	assertEndpointBlobRequests(t, blobs.requestSnapshot(), []string{
		amd64ConfigDigest,
		amd64LayerDigest,
		arm64ConfigDigest,
		arm64LayerDigest,
	})
	assertEndpointManifestRequests(t, manifests.requestSnapshot(), []string{"latest", amd64ChildDigest, arm64ChildDigest})
}

type endpointIndexChild struct {
	digest   string
	platform *ocispec.Platform
}

func endpointIndexManifestBody(t *testing.T, children ...endpointIndexChild) []byte {
	t.Helper()
	manifests := make([]ocispec.Descriptor, 0, len(children))
	for i := range children {
		manifests = append(manifests, ocispec.Descriptor{
			MediaType: distribution.MediaTypeOCIManifest,
			Digest:    digest.Digest(children[i].digest),
			Size:      512,
			Platform:  children[i].platform,
		})
	}
	return marshalEndpointManifest(t, ocispec.Index{
		MediaType: distribution.MediaTypeOCIIndex,
		Manifests: manifests,
	})
}

func linuxAMD64EndpointPlatform() *ocispec.Platform {
	return &ocispec.Platform{OS: "linux", Architecture: "amd64"}
}

func linuxARM64EndpointPlatform() *ocispec.Platform {
	return &ocispec.Platform{OS: "linux", Architecture: "arm64"}
}

func defaultEndpointPlatform() *ocispec.Platform {
	platform, err := platforms.Parse(config.DefaultContainerPrewarmPlatform())
	if err != nil {
		return linuxAMD64EndpointPlatform()
	}
	platform = platforms.Normalize(platform)
	return &platform
}

func nonDefaultEndpointPlatform() *ocispec.Platform {
	if defaultEndpointPlatform().Architecture == "arm64" {
		return linuxAMD64EndpointPlatform()
	}
	return linuxARM64EndpointPlatform()
}

func activateEndpointPrewarmPlatforms(t *testing.T, platformValues []string) {
	t.Helper()
	cfg := config.DefaultConfig()
	hub := cfg.Container["hub"]
	hub.Prewarm.Platforms = platformValues
	cfg.Container["hub"] = hub
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("activate container prewarm platforms: %v", err)
	}
	t.Cleanup(func() {
		defaultCfg := config.DefaultConfig()
		if err := defaultCfg.NormalizeAndValidate(); err != nil {
			t.Fatalf("restore container prewarm platforms: %v", err)
		}
	})
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
