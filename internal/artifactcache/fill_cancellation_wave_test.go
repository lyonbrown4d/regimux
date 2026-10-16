package artifactcache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
)

func TestCoalescedFillHealthyWaitersSurviveOwnerCancellation(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				runCanceledOwnerFillWave(t, mode)
			})
		})
	}
}

func runCanceledOwnerFillWave(t *testing.T, mode string) {
	t.Helper()
	ownerCtx, cancel := context.WithTimeout(t.Context(), time.Hour)
	defer cancel()
	wave := newFillCancellationWave()
	ownerResult := wave.startOwner(ownerCtx, func() error {
		<-ownerCtx.Done()
		return ownerCtx.Err()
	})
	<-wave.started
	results := wave.startWaiters(t.Context(), 512)
	wave.ready.Wait()
	if mode == "cancel" {
		cancel()
	} else {
		time.Sleep(time.Hour)
	}
	<-ownerCtx.Done()
	assertFillWaveErrors(t, ownerResult, 1, ownerCtx.Err())
	assertFillWaveErrors(t, results, 512, nil)
	wave.assertRefills(t, 1)
}

func TestCoalescedFillBackendDeadlineDoesNotStartRetryWave(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		wave := newFillCancellationWave()
		release := make(chan struct{})
		ownerResult := wave.startOwner(t.Context(), func() error {
			<-release
			return context.DeadlineExceeded
		})
		<-wave.started
		results := wave.startWaiters(t.Context(), 128)
		wave.ready.Wait()
		close(release)
		assertFillWaveErrors(t, ownerResult, 1, context.DeadlineExceeded)
		assertFillWaveErrors(t, results, 128, context.DeadlineExceeded)
		wave.assertRefills(t, 0)
	})
}

func TestCoalescedFillCanceledWaitersDoNotTakeOverOwner(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ownerCtx, cancelOwner := context.WithCancel(t.Context())
		defer cancelOwner()
		wave := newFillCancellationWave()
		ownerResult := wave.startOwner(ownerCtx, func() error {
			<-ownerCtx.Done()
			return ownerCtx.Err()
		})
		<-wave.started
		healthy := wave.startWaiters(t.Context(), 256)
		waiterCtx, cancelWaiters := context.WithCancel(t.Context())
		defer cancelWaiters()
		canceled := wave.startWaiters(waiterCtx, 256)
		wave.ready.Wait()
		cancelWaiters()
		cancelOwner()
		assertFillWaveErrors(t, ownerResult, 1, context.Canceled)
		assertFillWaveErrors(t, healthy, 256, nil)
		assertFillWaveErrors(t, canceled, 256, context.Canceled)
		wave.assertRefills(t, 1)
	})
}

func assertFillWaveErrors(t *testing.T, results <-chan error, count int, want error) {
	t.Helper()
	for index := range count {
		if err := <-results; !errors.Is(err, want) {
			t.Fatalf("fill result %d error = %v, want %v", index, err, want)
		}
	}
}

type fillCancellationWave struct {
	tracker *artifactcache.FillTracker
	key     artifactcache.Key
	started chan struct{}
	ready   sync.WaitGroup
	cached  atomic.Bool
	refills atomic.Int64
}

func (w *fillCancellationWave) assertRefills(t *testing.T, want int64) {
	t.Helper()
	if got := w.refills.Load(); got != want {
		t.Fatalf("replacement upstream fills = %d, want %d", got, want)
	}
}

func newFillCancellationWave() *fillCancellationWave {
	return &fillCancellationWave{
		tracker: artifactcache.NewFillTracker(),
		key:     artifactcache.Key{Alias: "test", Repository: "demo", Reference: "v1"},
		started: make(chan struct{}),
	}
}

func (w *fillCancellationWave) startOwner(ctx context.Context, finish func() error) <-chan error {
	result := make(chan error, 1)
	go func() {
		_, err := w.tracker.Coalesce(artifactcache.CoalesceRequest[string]{
			Context: ctx,
			Key:     w.key,
			Wait:    func() (string, bool, error) { return "", false, nil },
			Fill: func() (string, error) {
				close(w.started)
				return "", finish()
			},
		})
		result <- err
	}()
	return result
}

func (w *fillCancellationWave) startWaiters(ctx context.Context, count int) <-chan error {
	results := make(chan error, count)
	w.ready.Add(count)
	for range count {
		go func() {
			_, err := w.tracker.Coalesce(artifactcache.CoalesceRequest[string]{
				Context: &fillWaveWaitContext{Context: ctx, ready: &w.ready},
				Key:     w.key,
				Wait: func() (string, bool, error) {
					return "cached", w.cached.Load(), nil
				},
				Fill: func() (string, error) {
					w.refills.Add(1)
					w.cached.Store(true)
					return "origin", nil
				},
			})
			results <- err
		}()
	}
	return results
}

type fillWaveWaitContext struct {
	context.Context
	ready *sync.WaitGroup
	once  sync.Once
}

func (c *fillWaveWaitContext) Done() <-chan struct{} {
	c.once.Do(c.ready.Done)
	return c.Context.Done()
}
