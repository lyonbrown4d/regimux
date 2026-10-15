package pypi_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/arcgolabs/httpx"
	fiberadapter "github.com/arcgolabs/httpx/adapter/fiber"
	stdadapter "github.com/arcgolabs/httpx/adapter/std"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
)

func TestHTTPPackageFlushesHeadersBeforeUpstreamBody(t *testing.T) {
	t.Run("std", func(t *testing.T) { checkHeaderStreaming(t, startStdProxy) })
	t.Run("fiber", func(t *testing.T) { checkHeaderStreaming(t, startFiberProxy) })
}

func checkHeaderStreaming(t *testing.T, startProxy func(*testing.T, *pypi.Service) (*http.Client, string)) {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	releaseBody := func() { once.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusOK)
		requireNoError(t, "flush", http.NewResponseController(w).Flush())
		select {
		case <-release:
			writeResponse(t, w, "0123456789")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer releaseBody()
	service := newTestService(t.Context(), t, upstream.URL, nil)
	client, proxyURL := startProxy(t, service)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL+"/pypi/pypi/"+packageTailFor(t, upstream.URL, "/packages/demo.bin"), http.NoBody)
	requireNoError(t, "request", err)
	resp, err := client.Do(req)
	requireNoError(t, "headers before releasing body", err)
	defer closeBody(t, resp.Body)
	releaseBody()
	body, err := io.ReadAll(resp.Body)
	requireNoError(t, "stream body", err)
	if resp.StatusCode != http.StatusOK || string(body) != "0123456789" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
}

func startStdProxy(t *testing.T, service *pypi.Service) (*http.Client, string) {
	t.Helper()
	adapter := stdadapter.New(nil)
	server := httpx.New(httpx.WithAdapter(adapter))
	server.RegisterOnly(pypi.NewEndpoint(service))
	proxy := httptest.NewServer(adapter.Router())
	t.Cleanup(proxy.Close)
	return proxy.Client(), proxy.URL
}

func startFiberProxy(t *testing.T, service *pypi.Service) (*http.Client, string) {
	t.Helper()
	app := fiber.New()
	adapter := fiberadapter.New(app)
	server := httpx.New(httpx.WithAdapter(adapter))
	server.RegisterOnly(pypi.NewEndpoint(service))
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	requireNoError(t, "listen", err)
	stopped := make(chan error, 1)
	go func() { stopped <- app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		requireNoError(t, "shutdown Fiber", app.ShutdownWithContext(shutdownCtx))
		requireNoError(t, "Fiber listener", <-stopped)
	})
	return &http.Client{Timeout: 3 * time.Second}, "http://" + listener.Addr().String()
}
