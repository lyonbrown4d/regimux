package cache_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestManifestCanceledOwnerKeepsHealthyFollowersOnSharedFill(t *testing.T) {
	ctx := t.Context()
	const followers = 128
	body := []byte(`{"schemaVersion":2}`)
	client := &heldManifestCancellationClient{
		ownerEntered: make(chan struct{}, 128),
		release:      make(chan struct{}),
		body:         body,
		mediaType:    distribution.MediaTypeDockerManifest,
	}
	t.Cleanup(func() { client.releaseOnce.Do(func() { close(client.release) }) })
	proxy := newCancellationTestProxy(t, client)

	ownerCtx, cancelOwner := context.WithCancel(ctx)
	defer cancelOwner()
	ownerResult := make(chan manifestCancellationResult, 1)
	go func() {
		value, err := proxy.Manifests().Get(ownerCtx, cancellationManifestRequest())
		ownerResult <- manifestCancellationResult{value: value, err: err}
	}()
	waitForManifestCall(t, client.ownerEntered)

	results := launchManifestFollowers(ctx, t, proxy, followers)
	waitForManifestCancellationFlights(t, followers)
	cancelOwner()
	requireManifestCallerCanceled(t, ownerResult)
	requireManifestOriginCalls(t, client.calls.Load(), 1, "before shared fill release")
	client.releaseOnce.Do(func() { close(client.release) })
	requireHealthyManifestFollowers(t, results, followers, body)
	requireManifestOriginCalls(t, client.calls.Load(), 1, "after shared fill")
}

func TestManifestCanceledFollowerReturnsBeforeOwnerCompletes(t *testing.T) {
	ctx := t.Context()
	client := &heldManifestCancellationClient{
		ownerEntered: make(chan struct{}, 128),
		release:      make(chan struct{}),
		body:         []byte(`{"schemaVersion":2}`),
		mediaType:    distribution.MediaTypeDockerManifest,
	}
	t.Cleanup(func() { client.releaseOnce.Do(func() { close(client.release) }) })
	proxy := newCancellationTestProxy(t, client)

	ownerCtx, cancelOwner := context.WithCancel(ctx)
	defer cancelOwner()
	ownerResult := make(chan manifestCancellationResult, 1)
	go func() {
		value, err := proxy.Manifests().Get(ownerCtx, cancellationManifestRequest())
		ownerResult <- manifestCancellationResult{value: value, err: err}
	}()
	waitForManifestCall(t, client.ownerEntered)

	followerCtx, cancelFollower := context.WithCancel(ctx)
	followerResult := make(chan manifestCancellationResult, 1)
	go func() {
		value, err := proxy.Manifests().Get(followerCtx, cancellationManifestRequest())
		followerResult <- manifestCancellationResult{value: value, err: err}
	}()
	waitForManifestCancellationFlights(t, 1)
	cancelFollower()

	select {
	case got := <-followerResult:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled follower error = %v, want context canceled", got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled follower waited for the owner")
	}
	cancelOwner()
	select {
	case <-ownerResult:
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not return after cancellation")
	}
}

func TestManifestLiveOwnerBackendTimeoutDoesNotRetry(t *testing.T) {
	ctx := t.Context()
	const followers = 128
	client := &timeoutManifestClient{entered: make(chan struct{}, 1), release: make(chan struct{}), err: context.DeadlineExceeded}
	t.Cleanup(func() { client.releaseOnce.Do(func() { close(client.release) }) })
	proxy := newCancellationTestProxy(t, client)

	ownerResult := make(chan manifestCancellationResult, 1)
	go func() {
		value, err := proxy.Manifests().Get(ctx, cancellationManifestRequest())
		ownerResult <- manifestCancellationResult{value: value, err: err}
	}()
	waitForManifestCall(t, client.entered)
	results := launchManifestFollowers(ctx, t, proxy, followers)
	waitForManifestCancellationFlights(t, followers)
	client.releaseOnce.Do(func() { close(client.release) })

	if got := <-ownerResult; !errors.Is(got.err, context.DeadlineExceeded) {
		t.Fatalf("live owner backend error = %v, want deadline exceeded", got.err)
	}
	for i := range followers {
		select {
		case got := <-results:
			if !errors.Is(got.err, context.DeadlineExceeded) {
				t.Fatalf("follower %d backend error = %v, want deadline exceeded", i, got.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("backend timeout follower results incomplete: got %d of %d", i, followers)
		}
	}
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1; live-owner backend timeout must not retry", got)
	}
}

type manifestCancellationResult struct {
	value *cache.CachedManifest
	err   error
}

type heldManifestCancellationClient struct {
	*fakeRegistryClient
	ownerEntered chan struct{}
	release      chan struct{}
	releaseOnce  sync.Once
	calls        atomic.Int64
	body         []byte
	mediaType    string
}

func (c *heldManifestCancellationClient) GetManifest(ctx context.Context, _ upstream.GetManifestRequest) (*upstream.ManifestResponse, error) {
	c.calls.Add(1)
	c.ownerEntered <- struct{}{}
	select {
	case <-c.release:
		return cancellationManifestResponse(c.body, c.mediaType), nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for manifest release: %w", ctx.Err())
	}
}

type timeoutManifestClient struct {
	*fakeRegistryClient
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	calls       atomic.Int64
	err         error
}

func (c *timeoutManifestClient) GetManifest(context.Context, upstream.GetManifestRequest) (*upstream.ManifestResponse, error) {
	c.calls.Add(1)
	c.entered <- struct{}{}
	<-c.release
	return nil, c.err
}

func newCancellationTestProxy(t *testing.T, client upstream.RegistryClient) *cache.Proxy {
	t.Helper()
	metadata, objects := newTestStores(t)
	return newTestProxy(
		client,
		metadata,
		objects,
		backend.NewMemory(backend.MemoryOptions{}),
		config.Config{Cache: config.CacheConfig{Manifest: config.ManifestCacheConfig{TagTTL: time.Minute}}},
	)
}

func cancellationManifestRequest() cache.ManifestRequest {
	return cache.ManifestRequest{UpstreamAlias: "hub", Repo: "library/alpine", Reference: "latest", Method: http.MethodGet}
}

func cancellationManifestResponse(body []byte, mediaType string) *upstream.ManifestResponse {
	return &upstream.ManifestResponse{
		Body:      io.NopCloser(bytes.NewReader(body)),
		Digest:    testDigestFor(body),
		MediaType: mediaType,
		Size:      int64(len(body)),
		Headers:   http.Header{distribution.HeaderContentType: {mediaType}},
	}
}

func launchManifestFollowers(ctx context.Context, t *testing.T, proxy *cache.Proxy, count int) <-chan manifestCancellationResult {
	t.Helper()
	results := make(chan manifestCancellationResult, count)
	var ready sync.WaitGroup
	ready.Add(count)
	for range count {
		go func() {
			ready.Done()
			value, err := proxy.Manifests().Get(ctx, cancellationManifestRequest())
			results <- manifestCancellationResult{value: value, err: err}
		}()
	}
	ready.Wait()
	return results
}

func waitForManifestCancellationFlights(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var stacks bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
			t.Fatalf("write goroutine profile: %v", err)
		}
		if countManifestFlights(stacks.String()) >= want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("manifest singleflight waiters = fewer than %d", want)
}

func countManifestFlights(profile string) int {
	inFlight := 0
	for goroutine := range strings.SplitSeq(profile, "\n\n") {
		inManifestCall := strings.Contains(goroutine, "manifestProxy.Get") || strings.Contains(goroutine, "manifestProxy.coalesce")
		if inManifestCall && strings.Contains(goroutine, "singleflight") {
			inFlight++
		}
	}
	return inFlight
}

func requireManifestCallerCanceled(t *testing.T, result <-chan manifestCancellationResult) {
	t.Helper()
	select {
	case got := <-result:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled manifest caller error = %v, want context canceled", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled manifest caller did not return")
	}
}

func requireHealthyManifestFollowers(t *testing.T, results <-chan manifestCancellationResult, count int, body []byte) {
	t.Helper()
	for i := range count {
		select {
		case got := <-results:
			requireHealthyManifestResult(t, i, got, body)
		case <-time.After(15 * time.Second):
			t.Fatalf("healthy follower results incomplete: got %d of %d", i, count)
		}
	}
}

func requireHealthyManifestResult(t *testing.T, index int, result manifestCancellationResult, body []byte) {
	t.Helper()
	if result.err != nil {
		t.Fatalf("healthy follower %d: %v", index, result.err)
	}
	if result.value == nil || !bytes.Equal(result.value.Body, body) {
		t.Fatalf("healthy follower %d body = %v, want %q", index, result.value, body)
	}
}

func requireManifestOriginCalls(t *testing.T, got, want int64, phase string) {
	t.Helper()
	if got != want {
		t.Fatalf("upstream calls %s = %d, want %d", phase, got, want)
	}
}

var _ upstream.RegistryClient = (*heldManifestCancellationClient)(nil)
var _ upstream.RegistryClient = (*timeoutManifestClient)(nil)
