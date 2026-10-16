package meta

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPullWriteBatchLateMutationSurvivesEarlierRefreshFailure(t *testing.T) {
	hook := newPullWriteBatchHook()
	store := openPullWriteBatchStore(t, hook)
	hook.failRefresh.Store(true)
	defer hook.releaseGate()
	key := PullKey{Alias: "hub", Repository: "library/node", Reference: "late-refresh-failure"}
	at := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	owner := startPullBatchMutation(t.Context(), store, key, at)
	waitForPullWriteBatchHook(t, hook.entered)
	joined := make(chan struct{})
	followerCtx := &pullBatchJoinedContext{Context: t.Context(), joined: joined}
	follower := startPullBatchMutation(followerCtx, store, key, at.Add(time.Second))
	waitForPullWriteBatchHook(t, joined)
	hook.releaseGate()
	first := receivePullBatchMutation(t, owner)
	second := receivePullBatchMutation(t, follower)
	if !errors.Is(first.err, errPullWriteBatchInjected) {
		t.Fatalf("first batch error = %v, want injected refresh error", first.err)
	}
	if second.err != nil || second.record == nil || second.record.Count != 2 {
		t.Fatalf("unattempted late mutation lost after earlier refresh failure: record=%#v error=%v", second.record, second.err)
	}
	if got := hook.writes.Load(); got != 2 {
		t.Fatalf("pull writes = %d, want two distinct batches without replay", got)
	}
	if len(store.pullWriteBatches) != 0 {
		t.Fatalf("retained pull batch states = %d", len(store.pullWriteBatches))
	}
}

type pullBatchMutationResult struct {
	record *PullRecord
	err    error
}

func startPullBatchMutation(ctx context.Context, store *SQLStore, key PullKey, at time.Time) <-chan pullBatchMutationResult {
	result := make(chan pullBatchMutationResult, 1)
	go func() {
		record, err := store.RecordPull(ctx, key, at)
		result <- pullBatchMutationResult{record: record, err: err}
	}()
	return result
}

func receivePullBatchMutation(t *testing.T, result <-chan pullBatchMutationResult) pullBatchMutationResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("pull mutation did not finish")
		return pullBatchMutationResult{}
	}
}

type pullBatchJoinedContext struct {
	context.Context
	joined chan struct{}
	once   sync.Once
}

func (c *pullBatchJoinedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.joined) })
	return c.Context.Done()
}
