package cache_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestManifestConcurrentHeadAndGetKeepMethodSemantics(t *testing.T) {
	for _, ownerMethod := range []string{http.MethodHead, http.MethodGet} {
		t.Run(ownerMethod+"_owner", func(t *testing.T) {
			assertManifestMethodPair(t, ownerMethod)
		})
	}
}

type manifestMethodResult struct {
	method string
	value  *cache.CachedManifest
	err    error
}

func assertManifestMethodPair(t *testing.T, ownerMethod string) {
	t.Helper()
	ctx := t.Context()
	body := []byte(`{"schemaVersion":2}`)
	client := &heldManifestMethodClient{
		fakeRegistryClient: &fakeRegistryClient{manifestBody: body, manifestMedia: distribution.MediaTypeDockerManifest},
		entered:            make(chan struct{}, 2),
		release:            make(chan struct{}),
	}
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(client.release) }) })
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(client, metadata, objects, backend.NewMemory(backend.MemoryOptions{}),
		config.Config{Cache: config.CacheConfig{Manifest: config.ManifestCacheConfig{TagTTL: time.Minute}}})
	results := make(chan manifestMethodResult, 2)
	go callManifestMethod(ctx, proxy, ownerMethod, results)
	waitForManifestCall(t, client.entered)
	go callManifestMethod(ctx, proxy, otherManifestMethod(ownerMethod), results)
	waitForManifestCallsToReachFlight(t)
	releaseOnce.Do(func() { close(client.release) })
	seen := collectManifestMethodResults(t, results)
	assertManifestMethodBodies(t, seen, body)
}

func callManifestMethod(ctx context.Context, proxy *cache.Proxy, method string, results chan<- manifestMethodResult) {
	value, err := proxy.Manifests().Get(ctx, cache.ManifestRequest{
		UpstreamAlias: "hub", Repo: "library/alpine", Reference: "latest", Method: method,
	})
	results <- manifestMethodResult{method: method, value: value, err: err}
}

func otherManifestMethod(method string) string {
	if method == http.MethodGet {
		return http.MethodHead
	}
	return http.MethodGet
}

func collectManifestMethodResults(t *testing.T, results <-chan manifestMethodResult) map[string]*cache.CachedManifest {
	t.Helper()
	seen := make(map[string]*cache.CachedManifest, 2)
	for range 2 {
		select {
		case got := <-results:
			if got.err != nil {
				t.Fatalf("%s manifest get: %v", got.method, got.err)
			}
			seen[got.method] = got.value
		case <-time.After(5 * time.Second):
			t.Fatal("manifest calls did not complete after upstream release")
		}
	}
	return seen
}

func assertManifestMethodBodies(t *testing.T, seen map[string]*cache.CachedManifest, body []byte) {
	t.Helper()
	head := seen[http.MethodHead]
	if head == nil {
		t.Fatal("HEAD result is nil")
	}
	if len(head.Body) != 0 {
		t.Fatalf("HEAD result body = %q, want empty", head.Body)
	}
	get := seen[http.MethodGet]
	if get == nil {
		t.Fatal("GET result is nil")
	}
	if !bytes.Equal(get.Body, body) {
		t.Fatalf("GET result body = %q, want %q", get.Body, body)
	}
}

type heldManifestMethodClient struct {
	*fakeRegistryClient
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
}

func (c *heldManifestMethodClient) GetManifest(ctx context.Context, req upstream.GetManifestRequest) (*upstream.ManifestResponse, error) {
	c.entered <- struct{}{}
	select {
	case <-c.release:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for manifest release: %w", ctx.Err())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fakeRegistryClient.GetManifest(ctx, req)
}

func waitForManifestCall(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream manifest call did not start")
	}
}

func waitForManifestCallsToReachFlight(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if manifestInFlightCallCount(t) >= 2 {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("both manifest calls did not reach their singleflight requests")
}

func manifestInFlightCallCount(t *testing.T) int {
	t.Helper()
	var stacks bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
		t.Fatalf("write goroutine profile: %v", err)
	}
	inFlightCalls := 0
	for goroutine := range strings.SplitSeq(stacks.String(), "\n\n") {
		coalescedManifestCall := strings.Contains(goroutine, "manifestProxy.Get") || strings.Contains(goroutine, "manifestProxy.coalesce")
		if coalescedManifestCall && strings.Contains(goroutine, "singleflight") {
			inFlightCalls++
		}
	}
	return inFlightCalls
}

var _ upstream.RegistryClient = (*heldManifestMethodClient)(nil)
