package meta

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
)

func TestRecordPullRetriesCommittedCatalogRefreshFailure(t *testing.T) {
	hook := newPullCompletionHook()
	store := openPullCompletionStore(t, hook)
	hook.armed.Store(true)
	hook.failFirstRefresh.Store(true)
	key := PullKey{Alias: "pypi", Repository: "simple/retry", Reference: "stable"}

	record, err := store.RecordPull(t.Context(), key, time.Now().UTC())
	waitForPullCompletionSignal(t, hook.refreshFailure, "injected first refresh failure")
	if err != nil {
		t.Fatalf("record pull after one transient refresh failure: %v", err)
	}
	if record == nil || record.Count != 1 {
		t.Fatalf("record pull result = %#v, want count 1", record)
	}
	waitForPullCompletionSignal(t, hook.refreshCompleted, "catalog aggregate completion")
	assertPullCompletionCounts(t, store, key, 1, 1, 1)
	if got := hook.pullWrites.Load(); got != 1 {
		t.Fatalf("canonical pull writes = %d, want exactly one", got)
	}
	assertPullAndCatalogRefreshStatesReleased(t, store)
}

func TestRecordPullCompletesCommittedCatalogAfterCallerCancellation(t *testing.T) {
	hook := newPullCompletionHook()
	store := openPullCompletionStore(t, hook)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hook.cancelOnPullCommit = cancel
	hook.pauseAfterPullWrite.Store(true)
	hook.armed.Store(true)
	key := PullKey{Alias: "pypi", Repository: "simple/canceled", Reference: "stable"}
	result := make(chan error, 1)
	startContext := &pullCompletionStartContext{
		Context: ctx,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	t.Cleanup(hook.releasePullWrite)
	t.Cleanup(startContext.releaseStart)
	go func(callCtx context.Context) {
		_, err := store.RecordPull(callCtx, key, time.Now().UTC())
		result <- err
	}(startContext)
	waitForPullCompletionSignal(t, startContext.entered, "DoContext receipt registration")
	mutation := pullCompletionMutation(store, key)
	if mutation == nil {
		startContext.releaseStart()
		t.Fatal("pull mutation receipt was not registered before its callback")
	}
	startContext.releaseStart()
	waitForPullCompletionSignal(t, hook.pullCommitted, "canonical pull commit")
	if err := receivePullCompletionError(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("record pull error = %v, want caller cancellation", err)
	}
	secondJoined := make(chan struct{})
	secondContext := &pullBatchJoinedContext{Context: t.Context(), joined: secondJoined}
	secondResult := startPullBatchMutation(secondContext, store, key, time.Now().UTC().Add(time.Second))
	waitForPullCompletionSignal(t, secondJoined, "new caller joining while old callback is paused")
	hook.releasePullWrite()
	if second := receivePullBatchMutation(t, secondResult); second.err != nil || second.record == nil || second.record.Count != 2 {
		t.Fatalf("healthy caller after canceled leader: record=%#v error=%v", second.record, second.err)
	}
	assertCanceledPullCompletion(t, store, hook, key, mutation)
}

func assertCanceledPullCompletion(t *testing.T, store *SQLStore, hook *pullCompletionHook, key PullKey, mutation *pullWriteMutation) {
	t.Helper()
	receipt := waitForPullMutationReceipt(t, mutation)
	if receipt.err != nil || receipt.record.Count < 1 {
		t.Fatalf("canceled caller receipt = %#v, want completed first mutation", receipt)
	}
	waitForPullCompletionSignal(t, hook.refreshCompleted, "post-commit catalog completion")
	assertPullCompletionCounts(t, store, key, 2, 2, 2)
	if got := hook.pullWrites.Load(); got != 2 {
		t.Fatalf("canonical pull writes = %d, want exactly one per caller", got)
	}
	assertPullAndCatalogRefreshStatesReleased(t, store)
}

func pullCompletionMutation(store *SQLStore, key PullKey) *pullWriteMutation {
	store.pullWriteBatchMu.Lock()
	defer store.pullWriteBatchMu.Unlock()
	state := store.pullWriteBatches[key.String()]
	if state == nil {
		return nil
	}
	return state.mutation
}

func TestRecordPullCanceledBeforeCommitDoesNotWrite(t *testing.T) {
	hook := newPullCompletionHook()
	store := openPullCompletionStore(t, hook)
	hook.armed.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	key := PullKey{Alias: "pypi", Repository: "simple/pre-canceled", Reference: "stable"}

	if _, err := store.RecordPull(ctx, key, time.Now().UTC()); !errors.Is(err, context.Canceled) {
		t.Fatalf("record pull error = %v, want context cancellation", err)
	}
	if got := hook.pullWrites.Load(); got != 0 {
		t.Fatalf("canonical pull writes = %d, want zero", got)
	}
	if _, ok, err := store.Pull(t.Context(), key); err != nil || ok {
		t.Fatalf("pre-canceled pull persisted: found=%v error=%v", ok, err)
	}
	if _, err := store.UpstreamByAlias(t.Context(), key.Alias); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pre-canceled upstream lookup error = %v, want not found", err)
	}
	assertPullAndCatalogRefreshStatesReleased(t, store)
}

func openPullCompletionStore(t *testing.T, hook *pullCompletionHook) *SQLStore {
	t.Helper()
	store, err := OpenSQLiteWithOptions(t.Context(), DBOptions{
		Path:  filepath.Join(t.TempDir(), "pull-completion.db"),
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

func assertPullCompletionCounts(t *testing.T, store *SQLStore, key PullKey, pullCount, repositoryPulls, upstreamPulls int64) {
	t.Helper()
	pull, found, err := store.Pull(t.Context(), key)
	if err != nil || !found {
		t.Fatalf("read canonical pull: found=%v error=%v", found, err)
	}
	if pull.Count != pullCount {
		t.Fatalf("canonical pull count = %d, want %d", pull.Count, pullCount)
	}
	upstream, err := store.UpstreamByAlias(t.Context(), key.Alias)
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	repository, err := store.RepositoryByName(t.Context(), upstream.ID, key.Repository)
	if err != nil {
		t.Fatalf("read repository: %v", err)
	}
	if repository.PullCount != repositoryPulls || upstream.PullCount != upstreamPulls {
		t.Fatalf("catalog pull counts = repository:%d upstream:%d, want %d/%d", repository.PullCount, upstream.PullCount, repositoryPulls, upstreamPulls)
	}
}

func assertPullAndCatalogRefreshStatesReleased(t *testing.T, store *SQLStore) {
	t.Helper()
	store.pullWriteBatchMu.Lock()
	pullBatches := len(store.pullWriteBatches)
	store.pullWriteBatchMu.Unlock()
	if pullBatches != 0 {
		t.Fatalf("retained pull write batch states = %d, want zero", pullBatches)
	}
	store.catalogRefreshMu.Lock()
	catalogBatches := len(store.catalogRefreshes)
	store.catalogRefreshMu.Unlock()
	if catalogBatches != 0 {
		t.Fatalf("retained catalog refresh states = %d, want zero", catalogBatches)
	}
}

func waitForPullCompletionSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func receivePullCompletionError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("record pull did not return")
		return nil
	}
}

func waitForPullMutationReceipt(t *testing.T, mutation *pullWriteMutation) pullWriteBatchResult {
	t.Helper()
	select {
	case <-mutation.done:
		return *mutation.outcome
	case <-time.After(5 * time.Second):
		t.Fatal("post-commit pull mutation receipt did not complete")
		return pullWriteBatchResult{}
	}
}

type pullCompletionStartContext struct {
	context.Context
	entered     chan struct{}
	release     chan struct{}
	errCalls    atomic.Int32
	once        sync.Once
	releaseOnce sync.Once
}

func (c *pullCompletionStartContext) Err() error {
	if c.errCalls.Add(1) == 2 {
		c.once.Do(func() {
			close(c.entered)
			<-c.release
		})
	}
	switch c.Context.Err() {
	case context.Canceled:
		return context.Canceled
	case context.DeadlineExceeded:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (c *pullCompletionStartContext) releaseStart() {
	c.releaseOnce.Do(func() { close(c.release) })
}
