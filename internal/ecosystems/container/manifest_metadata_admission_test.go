package container_test

import (
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestRegistryManifestSlowPullRecordingReleasesMetadataAdmission(t *testing.T) {
	const requests = 5
	metadata := &blockedManifestPullStore{
		Store: newTestMetadata(t), entered: make(chan meta.PullKey, requests), release: make(chan struct{}),
	}
	backendCache := backend.NewMemory(backend.MemoryOptions{})
	body := endpointImageManifestBody(t, endpointTestDigest("c"))
	for i := range requests {
		seedAdmissionManifest(t, backendCache, strconv.Itoa(i), body)
	}
	proxy := cache.NewProxy(cache.ProxyDependencies{
		Cache: backendCache, Metadata: metadata, Logger: slog.New(slog.DiscardHandler),
	})
	endpoint := container.NewRegistryEndpoint(proxy.Manifests(), nil, nil, nil, slog.New(slog.DiscardHandler))
	baseURL := startAPIServer(t, endpoint)
	t.Cleanup(metadata.unblock)
	results := make(chan admissionManifestResponse, requests)
	startRequest := func(tag string) {
		go func() { results <- fetchAdmissionManifest(baseURL + "/v2/hub/library/alpine/manifests/" + tag) }()
	}
	for i := range requests - 1 {
		startRequest(strconv.Itoa(i))
	}
	for range requests - 1 {
		if !waitForAdmissionPull(metadata.entered, 5*time.Second) {
			t.Fatal("initial manifest requests did not reach pull recording")
		}
	}
	startRequest(strconv.Itoa(requests - 1))
	if !waitForAdmissionPull(metadata.entered, 2*time.Second) {
		t.Error("fifth manifest could not reach pull recording while four earlier metadata writes were blocked")
	}
	metadata.unblock()
	assertAdmissionResponses(t, results, requests, body)
	assertAdmissionPullCounts(t, metadata, requests)
}

func TestRegistryManifestSlowColdMetadataWritesReleaseAdmission(t *testing.T) {
	for _, operation := range []string{"upstream pull", "manifest upsert", "tag upsert"} {
		t.Run(operation, func(t *testing.T) { assertColdMetadataWriteReleasesAdmission(t, operation) })
	}
}

func assertColdMetadataWriteReleasesAdmission(t *testing.T, operation string) {
	t.Helper()
	const requests = 5
	baseMetadata := newTestMetadata(t)
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open object store: %v", err)
	}
	metadata := &blockedManifestMetadataStore{
		Store: baseMetadata, operation: operation,
		entered: make(chan struct{}, requests), release: make(chan struct{}),
	}
	client := &admissionManifestClient{body: endpointImageManifestBody(t, endpointTestDigest("c"))}
	proxy := cache.NewProxy(cache.ProxyDependencies{
		Client: client, Cache: backend.NewMemory(backend.MemoryOptions{}), Metadata: metadata, Objects: objects,
		Logger: slog.New(slog.DiscardHandler),
	})
	endpoint := container.NewRegistryEndpoint(proxy.Manifests(), nil, nil, nil, slog.New(slog.DiscardHandler))
	baseURL := startAPIServer(t, endpoint)
	t.Cleanup(metadata.unblock)
	results := make(chan admissionManifestResponse, requests)
	for i := range requests - 1 {
		startAdmissionRequest(results, baseURL, strconv.Itoa(i))
	}
	for range requests - 1 {
		if !waitForColdMetadataWrite(metadata.entered, 5*time.Second) {
			t.Fatalf("first four manifest requests did not block in %s", operation)
		}
	}
	startAdmissionRequest(results, baseURL, strconv.Itoa(requests-1))
	if !waitForAdmissionManifestCalls(client, requests, 2*time.Second) {
		t.Errorf("fifth manifest did not reach upstream while four %s writes were blocked", operation)
	}
	metadata.unblock()
	assertAdmissionResponses(t, results, requests, client.body)
}
