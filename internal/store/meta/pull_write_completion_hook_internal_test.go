package meta

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/arcgolabs/dbx"
)

type pullCompletionHook struct {
	armed                atomic.Bool
	failFirstRefresh     atomic.Bool
	pauseAfterPullWrite  atomic.Bool
	pullWrites           atomic.Int64
	cancelOnPullCommit   context.CancelFunc
	pullCommitted        chan struct{}
	pullWriteRelease     chan struct{}
	refreshFailure       chan struct{}
	refreshCompleted     chan struct{}
	commitOnce           sync.Once
	pullWriteOnce        sync.Once
	pullWriteReleaseOnce sync.Once
	failureOnce          sync.Once
	completionOnce       sync.Once
}

func newPullCompletionHook() *pullCompletionHook {
	return &pullCompletionHook{
		pullCommitted:    make(chan struct{}),
		pullWriteRelease: make(chan struct{}),
		refreshFailure:   make(chan struct{}),
		refreshCompleted: make(chan struct{}),
	}
}

func (h *pullCompletionHook) dbHook() dbx.Hook {
	return dbx.HookFuncs{BeforeFunc: h.before, AfterFunc: h.after}
}

func (h *pullCompletionHook) before(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	if !h.armed.Load() {
		return ctx, nil
	}
	if (event.Operation == dbx.OperationQuery || event.Operation == dbx.OperationQueryRow) &&
		isUpstreamAggregateQuery(event.SQL) && h.failFirstRefresh.CompareAndSwap(true, false) {
		h.failureOnce.Do(func() { close(h.refreshFailure) })
		return ctx, errPullCompletionInjected
	}
	return ctx, nil
}

func (h *pullCompletionHook) after(_ context.Context, event *dbx.HookEvent) {
	h.afterPullWrite(event)
	h.afterUpstreamAggregate(event)
}

func (h *pullCompletionHook) afterPullWrite(event *dbx.HookEvent) {
	if event.Operation == dbx.OperationExec && event.Err == nil && isPullWriteBatchInsert(event.SQL) {
		h.pullWrites.Add(1)
		h.pullWriteOnce.Do(func() {
			if h.cancelOnPullCommit != nil {
				h.commitOnce.Do(func() {
					close(h.pullCommitted)
					h.cancelOnPullCommit()
				})
			}
			if h.pauseAfterPullWrite.CompareAndSwap(true, false) {
				<-h.pullWriteRelease
			}
		})
	}
}

func (h *pullCompletionHook) afterUpstreamAggregate(event *dbx.HookEvent) {
	if event.Err == nil && (event.Operation == dbx.OperationQuery || event.Operation == dbx.OperationQueryRow) &&
		isUpstreamAggregateQuery(event.SQL) {
		h.completionOnce.Do(func() { close(h.refreshCompleted) })
	}
}

func (h *pullCompletionHook) releasePullWrite() {
	h.pullWriteReleaseOnce.Do(func() { close(h.pullWriteRelease) })
}

var errPullCompletionInjected = errors.New("injected post-commit catalog refresh failure")
