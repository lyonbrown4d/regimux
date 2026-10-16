package cache_test

import (
	"bytes"
	"context"
	"errors"
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

func TestBlobCanceledOwnerAllowsHealthyFollowerToFinish(t *testing.T) {
	body := []byte("stored OCI blob")
	digest := testDigestFor(body)
	client := &heldBlobCancellationClient{
		fakeRegistryClient: &fakeRegistryClient{},
		entered:            make(chan struct{}),
		release:            make(chan struct{}),
		body:               body,
		digest:             digest,
	}
	t.Cleanup(func() { client.releaseOnce.Do(func() { close(client.release) }) })
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(client, metadata, objects, backend.NewMemory(backend.MemoryOptions{}), config.Config{})
	request := cache.BlobRequest{UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet}

	ownerCtx, cancelOwner := context.WithCancel(t.Context())
	defer cancelOwner()
	ownerResult := make(chan blobCancellationResult, 1)
	go func() {
		result, err := proxy.Blobs().Get(ownerCtx, request)
		ownerResult <- blobCancellationResult{result: result, err: err}
	}()
	select {
	case <-client.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blob fill did not start")
	}

	followerResult := make(chan blobCancellationResult, 1)
	go func() {
		result, err := proxy.Blobs().Get(t.Context(), request)
		followerResult <- blobCancellationResult{result: result, err: err}
	}()
	waitForBlobCancellationFlight(t)
	cancelOwner()

	assertCanceledBlobOwner(t, ownerResult)
	client.releaseOnce.Do(func() { close(client.release) })
	assertBlobFollower(t, followerResult, body)
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("upstream blob fills = %d, want 1", got)
	}
}

func assertCanceledBlobOwner(t *testing.T, result <-chan blobCancellationResult) {
	t.Helper()
	select {
	case got := <-result:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled owner error = %v, want context canceled", got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled blob owner did not return")
	}
}

func assertBlobFollower(t *testing.T, result <-chan blobCancellationResult, body []byte) {
	t.Helper()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("healthy blob follower: %v", got.err)
		}
		if got.result == nil {
			t.Fatal("healthy blob follower returned no result")
		}
		content, err := io.ReadAll(got.result.Reader)
		if err != nil {
			t.Fatalf("read follower blob: %v", err)
		}
		if err := got.result.Reader.Close(); err != nil {
			t.Fatalf("close follower blob: %v", err)
		}
		if !bytes.Equal(content, body) {
			t.Fatalf("follower blob = %q, want %q", content, body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("healthy blob follower did not finish")
	}
}

type blobCancellationResult struct {
	result *cache.BlobReadResult
	err    error
}

type heldBlobCancellationClient struct {
	*fakeRegistryClient
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	calls       atomic.Int64
	body        []byte
	digest      string
}

func (c *heldBlobCancellationClient) ConsumeBlob(ctx context.Context, _ upstream.GetBlobRequest, consume upstream.BlobConsumeFunc) error {
	c.calls.Add(1)
	select {
	case <-c.entered:
	default:
		close(c.entered)
	}
	select {
	case <-c.release:
		body := io.NopCloser(bytes.NewReader(c.body))
		consumeErr := consume(&upstream.BlobResponse{
			Body:       body,
			Digest:     c.digest,
			Size:       int64(len(c.body)),
			StatusCode: http.StatusOK,
			Headers:    http.Header{distribution.HeaderContentType: {distribution.MediaTypeOctetStream}},
		})
		return errors.Join(consumeErr, body.Close())
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitForBlobCancellationFlight(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var stacks bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
			t.Fatalf("write goroutine profile: %v", err)
		}
		waiters := 0
		for goroutine := range strings.SplitSeq(stacks.String(), "\n\n") {
			if strings.Contains(goroutine, ".fetchStored(") && strings.Contains(goroutine, "singleflight") {
				waiters++
				if waiters == 2 {
					return
				}
			}
		}
		runtime.Gosched()
	}
	t.Fatal("blob follower did not join the in-flight fill")
}

var _ upstream.RegistryClient = (*heldBlobCancellationClient)(nil)
