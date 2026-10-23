package worker_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lyonbrown4d/regimux/internal/worker"
)

func TestLeaseRenewalDisabledPoolDoesNotSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pools := worker.NewPoolsConfig(0, 0, slog.New(slog.DiscardHandler))
		defer pools.Close()
		var calls atomic.Int32
		stop := worker.StartLeaseRenewal(context.Background(), pools.LeasePool(), time.Second, func(context.Context) {
			calls.Add(1)
		}, nil)
		time.Sleep(2 * time.Second)
		stop()
		if got := calls.Load(); got != 0 {
			t.Fatalf("disabled pool renewed %d times, want 0", got)
		}
	})
}
