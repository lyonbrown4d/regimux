package container_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/events"
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
	bus, fills := captureContainerPullFills(t)
	endpoint := container.NewRegistryEndpointFromOptions(
		&manifests,
		blobs,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
		container.RegistryEndpointOptions{Workers: pools, Events: bus},
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
	fill := receiveContainerPullFill(t, fills)
	if fill.Alias != "hub" || fill.Source != "worker" || fill.Kind != "blob" ||
		fill.Status != "saturated" || fill.Reason != "worker_pool_saturated" {
		t.Fatalf("unexpected container pull fill event: %#v", fill)
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

func captureContainerPullFills(t *testing.T) (events.Bus, <-chan events.ContainerPullFill) {
	t.Helper()
	bus := events.NewBus(slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := bus.Close(); err != nil {
			t.Fatalf("close bus: %v", err)
		}
	})
	received := make(chan events.ContainerPullFill, 1)
	unsubscribe, err := events.NewSubscriber(func(_ context.Context, event events.ContainerPullFill) error {
		received <- event
		return nil
	}).Subscribe(bus)
	if err != nil {
		t.Fatalf("subscribe container pull fill: %v", err)
	}
	t.Cleanup(unsubscribe)
	return bus, received
}

func receiveContainerPullFill(t *testing.T, received <-chan events.ContainerPullFill) events.ContainerPullFill {
	t.Helper()
	select {
	case event := <-received:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for container pull fill event")
		return events.ContainerPullFill{}
	}
}
