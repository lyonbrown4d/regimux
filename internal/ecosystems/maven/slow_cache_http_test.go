//nolint:gocognit,testpackage // This HTTP regression needs internal wiring and shared blocking-store fixtures.
package maven

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/httpx"
	fiberadapter "github.com/arcgolabs/httpx/adapter/fiber"
	stdadapter "github.com/arcgolabs/httpx/adapter/std"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const (
	slowCacheHTTPReference     = "/maven/central/com/acme/demo/1.0/demo-1.0.pom"
	slowCacheHTTPClientTimeout = time.Second
)

type slowCacheHTTPResult struct {
	status        int
	body          string
	firstByteTime time.Duration
	err           error
}

func TestMavenHTTPServesReadyBodyWhileCacheWriteIsBlocked(t *testing.T) {
	for _, adapter := range []struct {
		name  string
		start func(*testing.T, *Service) (*http.Client, string)
	}{
		{name: "std", start: startSlowCacheStdProxy},
		{name: "fiber", start: startSlowCacheFiberProxy},
	} {
		t.Run(adapter.name, func(t *testing.T) {
			fixture := newSlowCacheHTTPFixture(t, adapter.start)
			request := func() slowCacheHTTPResult { return fixture.request(t) }
			first := make(chan slowCacheHTTPResult, 1)
			go func() { first <- request() }()
			waitSlowCacheSignal(t, fixture.objects.entered, "initial object write")
			assertHealthySlowCacheHTTPResult(t, waitSlowCacheResult(t, first), "first request")

			// The caller has consumed the full response and its server request has ended,
			// while Put remains blocked. A same-key caller must also get the ready body.
			assertHealthySlowCacheHTTPResult(t, request(), "second same-key request")
			if got := fixture.upstreamRequests.Load(); got != 1 {
				t.Fatalf("upstream requests while cache write blocked = %d, want 1", got)
			}
			if _, ok, tagErr := fixture.metadata.Tag(t.Context(), slowCacheHTTPTagKey()); tagErr != nil || ok {
				t.Fatalf("tag before object Put completed: ok=%t err=%v, want absent", ok, tagErr)
			}

			fixture.releaseWrite()
			tag := waitSlowCacheTag(t, fixture.metadata)
			stored, _, err := fixture.local.Get(t.Context(), tag.Digest, object.GetOptions{})
			if err != nil {
				t.Fatalf("read committed object: %v", err)
			}
			storedBody, err := io.ReadAll(stored)
			if closeErr := stored.Close(); closeErr != nil {
				t.Errorf("close committed object: %v", closeErr)
			}
			if err != nil || string(storedBody) != slowWritePOM {
				t.Fatalf("committed object body = %q, %v; want complete POM", storedBody, err)
			}
			assertHealthySlowCacheHTTPResult(t, request(), "hot-cache request")
			if got := fixture.upstreamRequests.Load(); got != 1 {
				t.Fatalf("upstream requests after cache commit = %d, want 1", got)
			}
		})
	}
}

type slowCacheHTTPFixture struct {
	metadata         meta.Store
	local            object.Store
	objects          *blockingMavenWriteStore
	upstreamRequests atomic.Int32
	client           *http.Client
	proxyURL         string
	releaseWrite     func()
}

func newSlowCacheHTTPFixture(
	t *testing.T,
	startProxy func(*testing.T, *Service) (*http.Client, string),
) *slowCacheHTTPFixture {
	t.Helper()
	fixture := &slowCacheHTTPFixture{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fixture.upstreamRequests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		if _, writeErr := io.WriteString(w, slowWritePOM); writeErr != nil {
			t.Errorf("write upstream POM: %v", writeErr)
		}
	}))
	t.Cleanup(upstream.Close)
	metadata, err := meta.OpenSQLiteWithOptions(context.Background(), meta.DBOptions{
		Path: filepath.Join(t.TempDir(), "regimux.db"),
	})
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	fixture.metadata = metadata
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Errorf("close metadata: %v", closeErr)
		}
	})
	local, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open objects: %v", err)
	}
	fixture.local = local
	fixture.objects = &blockingMavenWriteStore{
		Store: local, entered: make(chan struct{}), release: make(chan struct{}),
	}
	service := NewService(ServiceDependencies{
		Config: config.Config{Maven: map[string]config.DependencyUpstreamConfig{
			"central": {Registry: upstream.URL},
		}},
		Metadata: metadata,
		Objects:  fixture.objects,
	})
	var releaseOnce sync.Once
	fixture.releaseWrite = func() { releaseOnce.Do(func() { close(fixture.objects.release) }) }
	t.Cleanup(func() {
		fixture.releaseWrite()
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("close Maven service: %v", closeErr)
		}
	})
	fixture.client, fixture.proxyURL = startProxy(t, service)
	// Release before adapter shutdown if an assertion fails while a handler is active.
	t.Cleanup(fixture.releaseWrite)
	return fixture
}

func (f *slowCacheHTTPFixture) request(t *testing.T) slowCacheHTTPResult {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.proxyURL+slowCacheHTTPReference, http.NoBody)
	if err != nil {
		return slowCacheHTTPResult{err: err}
	}
	started := time.Now()
	var firstByteNanos atomic.Int64
	trace := &httptrace.ClientTrace{GotFirstResponseByte: func() {
		firstByteNanos.Store(time.Since(started).Nanoseconds())
	}}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	resp, err := f.client.Do(req)
	if err != nil {
		return slowCacheHTTPResult{err: err}
	}
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr == nil {
		readErr = closeErr
	}
	return slowCacheHTTPResult{
		status: resp.StatusCode, body: string(body),
		firstByteTime: time.Duration(firstByteNanos.Load()), err: readErr,
	}
}

func startSlowCacheStdProxy(t *testing.T, service *Service) (*http.Client, string) {
	t.Helper()
	adapter := stdadapter.New(nil)
	server := httpx.New(httpx.WithAdapter(adapter))
	server.RegisterOnly(NewEndpoint(service))
	proxy := httptest.NewServer(adapter.Router())
	t.Cleanup(proxy.Close)
	return &http.Client{Timeout: slowCacheHTTPClientTimeout}, proxy.URL
}

func startSlowCacheFiberProxy(t *testing.T, service *Service) (*http.Client, string) {
	t.Helper()
	app := fiber.New()
	adapter := fiberadapter.New(app)
	server := httpx.New(httpx.WithAdapter(adapter))
	server.RegisterOnly(NewEndpoint(service))
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen Fiber: %v", err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if shutdownErr := app.ShutdownWithContext(ctx); shutdownErr != nil {
			t.Errorf("shutdown Fiber: %v", shutdownErr)
		}
		if listenerErr := <-stopped; listenerErr != nil {
			t.Errorf("Fiber listener: %v", listenerErr)
		}
	})
	return &http.Client{Timeout: slowCacheHTTPClientTimeout}, "http://" + listener.Addr().String()
}

func waitSlowCacheSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitSlowCacheResult(t *testing.T, result <-chan slowCacheHTTPResult) slowCacheHTTPResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("first HTTP response waited for blocked cache write")
		return slowCacheHTTPResult{}
	}
}

func assertHealthySlowCacheHTTPResult(t *testing.T, got slowCacheHTTPResult, label string) {
	t.Helper()
	if got.err != nil {
		t.Fatalf("%s: %v", label, got.err)
	}
	if got.status != http.StatusOK || got.body != slowWritePOM {
		t.Fatalf("%s: status=%d body=%q, want 200 and complete POM", label, got.status, got.body)
	}
	if got.firstByteTime <= 0 {
		t.Fatalf("%s: first response byte was not observed", label)
	}
	if got.firstByteTime > slowCacheHTTPClientTimeout {
		t.Fatalf("%s: first response byte took %s, beyond the existing %s client timeout", label, got.firstByteTime, slowCacheHTTPClientTimeout)
	}
	t.Logf("%s: first response byte after %s", label, got.firstByteTime)
}

func slowCacheHTTPTagKey() meta.TagKey {
	return meta.TagKey{Alias: "central", Repository: "com/acme/demo/1.0", Reference: "demo-1.0.pom"}
}

func waitSlowCacheTag(t *testing.T, metadata meta.Store) *meta.TagRecord {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		tag, ok, err := metadata.Tag(t.Context(), slowCacheHTTPTagKey())
		if err != nil {
			t.Fatalf("lookup committed tag: %v", err)
		}
		if ok {
			return tag
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatal("cache metadata was not committed after releasing object Put")
		}
	}
}
