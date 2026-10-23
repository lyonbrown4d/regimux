package artifactcache

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panjf2000/ants/v2"
)

func TestFillLeaseCapacityDoesNotBlockStartingAnotherFill(t *testing.T) {
	pool, err := ants.NewPool(1)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Release()
	tracker := NewFillTracker(WithFillLeaseScheduler(pool))
	finish := make(chan struct{})
	started := make(chan struct{}, 2)
	done := make(chan struct{}, 2)
	leases := [2]*capacityTestLease{{}, {}}
	defer finishCapacityFills(t, finish, done, leases)
	for _, lease := range leases {
		go func() {
			_, fillErr := tracker.fillWithLease(context.Background(), lease, func() (bool, error) {
				started <- struct{}{}
				<-finish
				return true, nil
			})
			if fillErr != nil {
				t.Errorf("fill: %v", fillErr)
			}
			done <- struct{}{}
		}()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("lease renewal capacity blocked the next fill before its callback started")
		}
	}
}

func finishCapacityFills(t *testing.T, finish chan<- struct{}, done <-chan struct{}, leases [2]*capacityTestLease) {
	t.Helper()
	close(finish)
	for range leases {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("fill did not finish after release")
		}
	}
	for _, lease := range leases {
		if got := lease.releases.Load(); got != 1 {
			t.Errorf("lease releases = %d, want 1", got)
		}
	}
}

type capacityTestLease struct {
	releases atomic.Int32
}

func (l *capacityTestLease) Release(context.Context) error {
	l.releases.Add(1)
	return nil
}

func (*capacityTestLease) Extend(context.Context, time.Duration) error { return nil }
