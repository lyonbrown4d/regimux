package meta

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
)

func recordPullKind(ctx context.Context, store *SQLStore, key PullKey, at time.Time, kind int) (*PullRecord, error) {
	switch kind {
	case 0, 1:
		return store.RecordPull(ctx, key, at)
	case 2:
		return store.RecordUpstreamPull(ctx, key, at)
	default:
		return store.RecordPolicyDeniedPull(ctx, key, at)
	}
}

func assertPullBatchRecord(t *testing.T, record *PullRecord, count, denied int64, lastPull, lastUpstream, lastDenied time.Time) {
	t.Helper()
	if record == nil || record.Count != count || record.PolicyDeniedCount != denied ||
		!record.LastPullAt.Equal(lastPull) || !record.LastUpstreamPullAt.Equal(lastUpstream) ||
		!record.LastPolicyDeniedAt.Equal(lastDenied) {
		t.Fatalf("unexpected batched pull record: %#v", record)
	}
}

func pullBatchRecordCovers(record *PullRecord, kind int, at time.Time) bool {
	switch kind {
	case 0, 1:
		return record.Count > 0 && !record.LastPullAt.Before(at)
	case 2:
		return !record.LastUpstreamPullAt.Before(at)
	default:
		return record.PolicyDeniedCount > 0 && !record.LastPolicyDeniedAt.Before(at)
	}
}

func openPullWriteBatchStore(t *testing.T, hook *pullWriteBatchHook) *SQLStore {
	t.Helper()
	store, err := OpenSQLiteWithOptions(t.Context(), DBOptions{
		Path:  filepath.Join(t.TempDir(), "pull-write-batch.db"),
		Hooks: []dbx.Hook{hook.dbHook()},
	})
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close metadata store: %v", err)
		}
	})
	return store
}

func waitForPullWriteBatchUsers(t *testing.T, store *SQLStore, key string, users int) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		store.pullWriteBatchMu.Lock()
		state := store.pullWriteBatches[key]
		got := 0
		if state != nil {
			got = state.users
		}
		store.pullWriteBatchMu.Unlock()
		if got == users {
			return true
		}
		if state == nil {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func waitForPullWriteBatchProgress(t *testing.T, store *SQLStore, key string, users int, hook *pullWriteBatchHook) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		store.pullWriteBatchMu.Lock()
		state := store.pullWriteBatches[key]
		got := 0
		if state != nil {
			got = state.users
		}
		store.pullWriteBatchMu.Unlock()
		if got == users || hook.attempts.Load() >= int64(users) {
			return
		}
		if state == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d concurrent pull attempts", users)
}

func waitForPullWriteBatchHook(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for pull batch write")
	}
}

type pullWriteBatchHook struct {
	entered     chan struct{}
	release     chan struct{}
	first       sync.Once
	releaseOnce sync.Once
	writes      atomic.Int64
	attempts    atomic.Int64
	failFirst   atomic.Bool
	failRefresh atomic.Bool
}

func newPullWriteBatchHook() *pullWriteBatchHook {
	return &pullWriteBatchHook{entered: make(chan struct{}), release: make(chan struct{})}
}

func (h *pullWriteBatchHook) dbHook() dbx.Hook {
	return dbx.HookFuncs{BeforeFunc: h.before, AfterFunc: h.after}
}

func (h *pullWriteBatchHook) before(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	if strings.Contains(strings.ToLower(event.SQL), "meta_upstreams") && h.failRefresh.CompareAndSwap(true, false) {
		return ctx, errPullWriteBatchInjected
	}
	if event.Operation != dbx.OperationExec || !isPullWriteBatchInsert(event.SQL) {
		return ctx, nil
	}
	h.attempts.Add(1)
	h.first.Do(func() { close(h.entered) })
	if h.failFirst.CompareAndSwap(true, false) {
		return ctx, errPullWriteBatchInjected
	}
	select {
	case <-h.release:
		return ctx, nil
	case <-ctx.Done():
		return ctx, fmt.Errorf("wait for pull batch test hook: %w", ctx.Err())
	}
}

var errPullWriteBatchInjected = errors.New("injected pull write failure")

func (h *pullWriteBatchHook) releaseGate() {
	h.releaseOnce.Do(func() { close(h.release) })
}

func (h *pullWriteBatchHook) after(_ context.Context, event *dbx.HookEvent) {
	if event.Operation == dbx.OperationExec && isPullWriteBatchInsert(event.SQL) {
		h.writes.Add(1)
	}
}

func isPullWriteBatchInsert(statement string) bool {
	sql := strings.ToLower(strings.TrimSpace(statement))
	return strings.HasPrefix(sql, "insert") && strings.Contains(sql, "meta_pulls")
}
