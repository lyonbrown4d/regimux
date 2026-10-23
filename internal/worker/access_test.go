package worker_test

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/panjf2000/ants/v2"
	"github.com/stretchr/testify/require"
)

func TestAccessPoolBoundsWorkAndRejectsAfterClose(t *testing.T) {
	for _, test := range []struct {
		name          string
		ioConcurrency int
		accepted      int
	}{
		{name: "configured limit", ioConcurrency: 1, accepted: 1},
		{name: "access limit", ioConcurrency: 16, accepted: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertAccessPoolBoundsAndClose(t, test.ioConcurrency, test.accepted)
		})
	}
}

func assertAccessPoolBoundsAndClose(t *testing.T, ioConcurrency, accepted int) {
	t.Helper()
	pools := worker.NewPoolsConfig(ioConcurrency, 0, slog.New(slog.DiscardHandler))
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { finish(); pools.Close() })
	started := make(chan struct{}, accepted)
	var completed atomic.Int64
	for range accepted {
		require.NoError(t, pools.SubmitAccess(func() {
			started <- struct{}{}
			<-release
			completed.Add(1)
		}))
	}
	for range accepted {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("access worker did not start")
		}
	}
	rejected := make(chan error, 1)
	var unexpected atomic.Bool
	go func() { rejected <- pools.SubmitAccess(func() { unexpected.Store(true) }) }()
	select {
	case err := <-rejected:
		require.ErrorIs(t, err, ants.ErrPoolOverload)
	case <-time.After(time.Second):
		t.Fatal("saturated access pool blocked the caller")
	}
	finish()
	pools.Close()
	require.EqualValues(t, accepted, completed.Load(), "close must drain accepted work")
	require.ErrorIs(t, pools.SubmitAccess(func() { unexpected.Store(true) }), ants.ErrPoolClosed)
	require.False(t, unexpected.Load())
}

func TestAccessPoolDisabledAndNilRejectWork(t *testing.T) {
	disabled := worker.NewPoolsConfig(0, 0, slog.New(slog.DiscardHandler))
	t.Cleanup(disabled.Close)
	for _, pools := range []*worker.Pools{nil, disabled} {
		var called atomic.Bool
		require.ErrorIs(t, pools.SubmitAccess(func() { called.Store(true) }), ants.ErrPoolClosed)
		require.False(t, called.Load())
		require.Error(t, pools.SubmitAccess(nil))
	}
}
