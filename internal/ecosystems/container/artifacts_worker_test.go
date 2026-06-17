package container_test

import (
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestRegistryEndpointManifestFillSkipsWhenWorkerPoolSaturated(t *testing.T) {
	configDigest := endpointTestDigest("c")
	manifests := endpointManifestService{
		manifest: cachedEndpointManifest(
			endpointTestDigest("m"),
			distribution.MediaTypeOCIManifest,
			endpointImageManifestBody(t, configDigest),
		),
	}
	blobs := newEndpointBlobService()
	pools := worker.NewPoolsConfig(1, 0, slog.New(slog.DiscardHandler))
	defer pools.Close()
	endpoint := container.NewRegistryEndpointFromOptions(
		&manifests,
		blobs,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
		container.RegistryEndpointOptions{Workers: pools},
	)
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d body=%q, want 200", resp.StatusCode, body)
	}
	blobs.waitRequests(t, 1)

	resp = httpGet(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	body = readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d body=%q, want 200", resp.StatusCode, body)
	}
	assertEndpointBlobRequestCount(t, blobs, 1)

	blobs.release()
	blobs.waitClosed(t, 1)
	assertEndpointBlobRequests(t, blobs.requestSnapshot(), []string{configDigest})
}

func assertEndpointBlobRequestCount(t *testing.T, blobs *endpointBlobService, want int) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
	if requests := blobs.requestSnapshot(); len(requests) != want {
		t.Fatalf("blob requests = %d, want %d: %#v", len(requests), want, requests)
	}
}
