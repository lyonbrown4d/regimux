package artifactcache_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
)

func TestCoalesceFillPropagatesOwnerErrorToFollower(t *testing.T) {
	t.Parallel()

	tracker := artifactcache.NewFillTracker()
	key := artifactcache.Key{
		Alias:      "central",
		Repository: "org/example/demo/1.0",
		Reference:  "demo-1.0.pom",
	}
	current, owner := tracker.Begin(key)
	if !owner {
		t.Fatal("first fill did not become owner")
	}

	enteredWait := make(chan struct{})
	ctx := &waitSignalContext{
		Context: context.Background(),
		entered: enteredWait,
	}
	result := make(chan error, 1)
	waitCalled := false
	fillCalled := false
	go func() {
		_, err := artifactcache.CoalesceFill(
			ctx,
			tracker,
			key,
			func() (string, bool, error) {
				waitCalled = true
				return "", false, nil
			},
			func() (string, error) {
				fillCalled = true
				return "", nil
			},
		)
		result <- err
	}()

	<-enteredWait
	ownerErr := errors.New("upstream failed")
	tracker.Finish(key, current, ownerErr)
	err := <-result

	if !errors.Is(err, ownerErr) {
		t.Fatalf("error = %v, want owner error", err)
	}
	if waitCalled {
		t.Fatal("cache lookup ran after owner failure")
	}
	if fillCalled {
		t.Fatal("follower started another fill")
	}
}

type waitSignalContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *waitSignalContext) Done() <-chan struct{} {
	c.once.Do(func() {
		close(c.entered)
	})
	return c.Context.Done()
}
