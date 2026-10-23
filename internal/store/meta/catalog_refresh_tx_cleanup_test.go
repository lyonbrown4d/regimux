package meta_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

var errCatalogRefreshInjected = errors.New("forced catalog refresh failure")
var errCatalogCommitInjected = errors.New("forced catalog commit failure")

func TestCatalogRefreshCommitDoesNotRollback(t *testing.T) {
	hook := &catalogTxCleanupHook{}
	store := openCatalogTxCleanupStore(t, hook)
	if _, err := store.RecordPull(t.Context(), catalogCleanupPullKey(), time.Now().UTC()); err != nil {
		t.Fatalf("record pull: %v", err)
	}
	if got := hook.commits.Load(); got != 1 {
		t.Fatalf("commit transactions = %d, want 1", got)
	}
	if got := hook.rollbacks.Load(); got != 0 {
		t.Fatalf("rollback transactions after commit = %d, want 0", got)
	}
}

func TestCatalogRefreshFailureRollsBack(t *testing.T) {
	hook := &catalogTxCleanupHook{failLockedUpstreamRead: true}
	store := openCatalogTxCleanupStore(t, hook)
	_, err := store.RecordPull(t.Context(), catalogCleanupPullKey(), time.Now().UTC())
	if !errors.Is(err, errCatalogRefreshInjected) {
		t.Fatalf("record pull error = %v, want forced catalog refresh failure", err)
	}
	if got := hook.rollbacks.Load(); got != 2 {
		t.Fatalf("rollback transactions after two failed attempts = %d, want 2", got)
	}
	if got := hook.commits.Load(); got != 0 {
		t.Fatalf("commit transactions after failure = %d, want 0", got)
	}
	if _, ok, pullErr := store.Pull(t.Context(), catalogCleanupPullKey()); pullErr != nil || !ok {
		t.Fatalf("pull upsert should remain committed before catalog refresh: found=%v error=%v", ok, pullErr)
	}
}

func TestCatalogRefreshCompletesAfterCallerCancellation(t *testing.T) {
	hook := &catalogTxCleanupHook{commitDone: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hook.cancelRequest = cancel
	store := openCatalogTxCleanupStore(t, hook)
	_, err := store.RecordPull(ctx, catalogCleanupPullKey(), time.Now().UTC())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("record pull error = %v, want context canceled", err)
	}
	select {
	case <-hook.commitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("post-commit catalog refresh did not finish after caller cancellation")
	}
	assertCatalogTxCounts(t, hook, 1, 0)
	assertCatalogPullPersisted(t, store)
}

func TestCatalogRefreshCommitFailureRollsBack(t *testing.T) {
	hook := &catalogTxCleanupHook{failCommit: true}
	store := openCatalogTxCleanupStore(t, hook)
	_, err := store.RecordPull(t.Context(), catalogCleanupPullKey(), time.Now().UTC())
	if !errors.Is(err, errCatalogCommitInjected) {
		t.Fatalf("record pull error = %v, want forced catalog commit failure", err)
	}
	assertCatalogTxCounts(t, hook, 2, 2)
	assertCatalogPullPersisted(t, store)
}

func openCatalogTxCleanupStore(t *testing.T, hook *catalogTxCleanupHook) *meta.SQLStore {
	t.Helper()
	store, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{
		Path:  filepath.Join(t.TempDir(), "catalog-refresh.db"),
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

func catalogCleanupPullKey() meta.PullKey {
	return meta.PullKey{Alias: "pypi", Repository: "packages/demo", Reference: "1.2"}
}

type catalogTxCleanupHook struct {
	commits                atomic.Int32
	rollbacks              atomic.Int32
	aggregateAttempts      atomic.Int32
	commitAttempts         atomic.Int32
	failLockedUpstreamRead bool
	failCommit             bool
	cancelRequest          context.CancelFunc
	cancelOnce             sync.Once
	commitDone             chan struct{}
	commitDoneOnce         sync.Once
}

func (h *catalogTxCleanupHook) dbHook() dbx.Hook {
	return dbx.HookFuncs{BeforeFunc: h.before, AfterFunc: h.after}
}

func (h *catalogTxCleanupHook) before(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	if h.failCommit && event.Operation == dbx.OperationCommitTx && h.commitAttempts.Add(1) <= 2 {
		return ctx, errCatalogCommitInjected
	}
	if h.failLockedUpstreamRead && isCatalogQuery(event) && isCatalogAggregateQuery(event.SQL) && h.aggregateAttempts.Add(1) <= 2 {
		return ctx, errCatalogRefreshInjected
	}
	if isCatalogUpstreamQuery(event) && h.cancelRequest != nil {
		h.cancelOnce.Do(h.cancelRequest)
	}
	return ctx, nil
}

func isCatalogUpstreamQuery(event *dbx.HookEvent) bool {
	return (event.Operation == dbx.OperationQuery || event.Operation == dbx.OperationQueryRow) &&
		strings.Contains(strings.ToLower(event.SQL), "meta_upstreams")
}

func isCatalogQuery(event *dbx.HookEvent) bool {
	return event.Operation == dbx.OperationQuery || event.Operation == dbx.OperationQueryRow
}

func isCatalogAggregateQuery(statement string) bool {
	sql := strings.ToLower(statement)
	return strings.Contains(sql, "meta_repositories") &&
		strings.Contains(sql, "count(") && strings.Contains(sql, "sum(")
}

func assertCatalogTxCounts(t *testing.T, hook *catalogTxCleanupHook, commits, rollbacks int32) {
	t.Helper()
	if got := hook.commits.Load(); got != commits {
		t.Errorf("commit transactions = %d, want %d", got, commits)
	}
	if got := hook.rollbacks.Load(); got != rollbacks {
		t.Errorf("rollback transactions = %d, want %d", got, rollbacks)
	}
}

func assertCatalogPullPersisted(t *testing.T, store *meta.SQLStore) {
	t.Helper()
	record, ok, err := store.Pull(t.Context(), catalogCleanupPullKey())
	if err != nil || !ok {
		t.Errorf("pull upsert should remain committed before catalog refresh: found=%v error=%v", ok, err)
		return
	}
	if record.Count != 1 {
		t.Errorf("canonical pull count = %d, want exactly one after refresh retries", record.Count)
	}
}

func (h *catalogTxCleanupHook) after(_ context.Context, event *dbx.HookEvent) {
	if event.Operation == dbx.OperationCommitTx {
		h.commits.Add(1)
		if h.commitDone != nil {
			h.commitDoneOnce.Do(func() { close(h.commitDone) })
		}
		return
	}
	if event.Operation == dbx.OperationRollbackTx {
		h.rollbacks.Add(1)
		return
	}
}
