package suggestion_test

import (
	"context"
	"errors"
	"io"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/suggestion"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
)

func TestRefreshTagsCanceledOwnerAllowsHealthyFollowerToFinish(t *testing.T) {
	client := &heldTagCancellationClient{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { client.releaseOnce.Do(func() { close(client.release) }) })
	service := suggestion.NewService(suggestion.Dependencies{
		Client: client,
		Cache:  backend.NewMemory(backend.MemoryOptions{}),
	})
	request := manifestReq()

	ownerCtx, cancelOwner := context.WithCancel(t.Context())
	defer cancelOwner()
	ownerResult := make(chan tagRefreshCancellationResult, 1)
	go func() {
		index, err := service.RefreshTags(ownerCtx, request)
		ownerResult <- tagRefreshCancellationResult{index: index, err: err}
	}()
	select {
	case <-client.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("tag refresh did not start")
	}

	followerResult := make(chan tagRefreshCancellationResult, 1)
	go func() {
		index, err := service.RefreshTags(t.Context(), request)
		followerResult <- tagRefreshCancellationResult{index: index, err: err}
	}()
	waitForTagCancellationFlight(t)
	cancelOwner()

	select {
	case got := <-ownerResult:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled tag refresh owner error = %v, want context canceled", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled tag refresh owner did not return")
	}
	client.releaseOnce.Do(func() { close(client.release) })

	select {
	case got := <-followerResult:
		if got.err != nil {
			t.Fatalf("healthy tag refresh follower: %v", got.err)
		}
		if got.index == nil || len(got.index.Tags) != 1 || got.index.Tags[0] != "stable" {
			t.Fatalf("follower tag index = %#v, want [stable]", got.index)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("healthy tag refresh follower did not finish")
	}
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("tag list calls = %d, want 1", got)
	}
}

type tagRefreshCancellationResult struct {
	index *suggestion.TagIndex
	err   error
}

type heldTagCancellationClient struct {
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	calls       atomic.Int64
}

func (c *heldTagCancellationClient) ListTags(ctx context.Context, _ upstream.ListTagsRequest) (*upstream.TagsResponse, error) {
	c.calls.Add(1)
	select {
	case <-c.entered:
	default:
		close(c.entered)
	}
	select {
	case <-c.release:
		return &upstream.TagsResponse{Body: io.NopCloser(strings.NewReader(`{"tags":["stable"]}`))}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func waitForTagCancellationFlight(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var stacks strings.Builder
		if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
			t.Fatalf("write goroutine profile: %v", err)
		}
		waiters := 0
		for goroutine := range strings.SplitSeq(stacks.String(), "\n\n") {
			if strings.Contains(goroutine, ".RefreshTags(") && strings.Contains(goroutine, "singleflight") {
				waiters++
				if waiters == 2 {
					return
				}
			}
		}
		runtime.Gosched()
	}
	t.Fatal("tag refresh follower did not join the in-flight refresh")
}

var _ suggestion.TagClient = (*heldTagCancellationClient)(nil)
