package coalescer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/coalescer"
)

func TestTrackerCoalescesByKey(t *testing.T) {
	tracker := coalescer.NewTracker()
	ownerFill, owner := tracker.Begin("blob")
	if !owner {
		t.Fatal("first begin should own fill")
	}
	waiterFill, owner := tracker.Begin("blob")
	if owner {
		t.Fatal("second begin should wait for fill")
	}
	if ownerFill != waiterFill {
		t.Fatal("waiter did not receive active fill")
	}

	wantErr := errors.New("fill failed")
	done := make(chan error, 1)
	go func() {
		done <- waiterFill.Wait(context.Background())
	}()

	tracker.Finish("blob", ownerFill, wantErr)
	if err := <-done; !errors.Is(err, wantErr) {
		t.Fatalf("wait error = %v, want %v", err, wantErr)
	}
}

func TestTrackerCurrent(t *testing.T) {
	tracker := coalescer.NewTracker()
	fill, owner := tracker.Begin("blob")
	if !owner {
		t.Fatal("first begin should own fill")
	}
	current, ok := tracker.Current("blob")
	if !ok || current != fill {
		t.Fatalf("current fill = %p ok=%v, want %p true", current, ok, fill)
	}
	tracker.Finish("blob", fill, nil)
	if _, ok := tracker.Current("blob"); ok {
		t.Fatal("current fill should be removed after finish")
	}
}
