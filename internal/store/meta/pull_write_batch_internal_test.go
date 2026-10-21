package meta

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSQLStoreRecordPullBatchesConcurrentMutations(t *testing.T) {
	const workers = 512
	hook := newPullWriteBatchHook()
	store := openPullWriteBatchStore(t, hook)
	key := PullKey{Alias: "hub", Repository: "library/node", Reference: "20"}
	started := make(chan struct{})
	results := make([]*PullRecord, workers)
	errs := make([]error, workers)
	base := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	var group sync.WaitGroup
	defer func() {
		hook.releaseGate()
		group.Wait()
	}()
	for index := range workers {
		group.Go(func() {
			<-started
			at := base.Add(time.Duration(index) * time.Nanosecond)
			results[index], errs[index] = recordPullKind(t.Context(), store, key, at, index%4)
		})
	}
	close(started)
	waitForPullWriteBatchHook(t, hook.entered)
	waitForPullWriteBatchProgress(t, store, key.String(), workers, hook)
	hook.releaseGate()
	group.Wait()

	assertPullBatchResults(t, errs, results, base)
	assertPullBatchWriteCount(t, hook, workers)
	results[0].Count = -1
	if results[1].Count < 1 {
		t.Fatalf("mutating one caller result changed another result: count=%d", results[1].Count)
	}
	assertPersistedPullBatch(t, store, key, base)
}

func TestSQLStoreRecordPullBatchCancellationDoesNotPoisonFollower(t *testing.T) {
	hook := newPullWriteBatchHook()
	store := openPullWriteBatchStore(t, hook)
	key := PullKey{Alias: "hub", Repository: "library/node", Reference: "cancel"}
	leaderCtx, cancelLeader := context.WithCancel(t.Context())
	defer cancelLeader()
	base := time.Date(2026, 5, 26, 11, 0, 0, 0, time.UTC)
	leaderErr := make(chan error, 1)
	var group sync.WaitGroup
	defer func() {
		hook.releaseGate()
		group.Wait()
	}()
	group.Go(func() {
		_, err := store.RecordPull(leaderCtx, key, base)
		leaderErr <- err
	})
	waitForPullWriteBatchHook(t, hook.entered)
	followerResult := make(chan *PullRecord, 1)
	followerErr := make(chan error, 1)
	followerJoined := make(chan struct{})
	followerCtx := &pullBatchJoinedContext{Context: t.Context(), joined: followerJoined}
	group.Go(func() {
		record, err := store.RecordPolicyDeniedPull(followerCtx, key, base.Add(time.Second))
		followerResult <- record
		followerErr <- err
	})
	if !waitForPullWriteBatchUsers(t, store, key.String(), 2) {
		hook.releaseGate()
		<-leaderErr
		if err := <-followerErr; err != nil {
			t.Fatalf("baseline follower failed after leader cancellation: %v", err)
		}
		t.Skip("per-key pull batching is not present in the baseline")
	}
	waitForPullWriteBatchHook(t, followerJoined)
	cancelLeader()
	assertPullBatchError(t, leaderErr, context.Canceled, "canceled caller did not leave the shared batch", "canceled leader")
	hook.releaseGate()
	assertPullBatchError(t, followerErr, nil, "healthy follower did not finish the shared batch", "healthy follower")
	if record := <-followerResult; record == nil || record.Count != 1 || record.PolicyDeniedCount != 1 {
		t.Fatalf("healthy follower returned incomplete persisted record: %#v", record)
	}
	if got := hook.writes.Load(); got < 1 || got > 2 {
		t.Fatalf("pull upsert statements after cancellation = %d, want one or two", got)
	}
}

func assertPullBatchResults(t *testing.T, errs []error, results []*PullRecord, base time.Time) {
	t.Helper()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("record pull %d: %v", index, err)
		}
		at := base.Add(time.Duration(index) * time.Nanosecond)
		if results[index] == nil || !pullBatchRecordCovers(results[index], index%4, at) {
			t.Fatalf("caller %d received a record that does not cover its mutation: %#v", index, results[index])
		}
	}
	if results[0] == results[1] {
		t.Fatal("callers shared a mutable PullRecord pointer")
	}
}

func assertPullBatchWriteCount(t *testing.T, hook *pullWriteBatchHook, workers int) {
	t.Helper()
	if got := hook.writes.Load(); got > 2 {
		t.Fatalf("pull upsert statements = %d, want at most two batches for %d calls", got, workers)
	}
}

func assertPersistedPullBatch(t *testing.T, store *SQLStore, key PullKey, base time.Time) {
	t.Helper()
	stored, ok, err := store.Pull(t.Context(), key)
	if err != nil || !ok {
		t.Fatalf("read canonical pull row: found=%v error=%v", ok, err)
	}
	assertPullBatchRecord(t, stored, 256, 128, base.Add(509*time.Nanosecond), base.Add(510*time.Nanosecond), base.Add(511*time.Nanosecond))
	if len(store.pullWriteBatches) != 0 {
		t.Fatalf("pull batch state retained after callers completed: %d entries", len(store.pullWriteBatches))
	}
}

func assertPullBatchError(t *testing.T, result <-chan error, want error, timeoutMessage, label string) {
	t.Helper()
	select {
	case err := <-result:
		if want == nil && err != nil {
			t.Fatalf("%s error = %v", label, err)
		}
		if want != nil && !errors.Is(err, want) {
			t.Fatalf("%s error = %v, want %v", label, err, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal(timeoutMessage)
	}
}

func TestSQLStoreRecordPullBatchDoesNotReplayFailedWrite(t *testing.T) {
	hook := newPullWriteBatchHook()
	hook.failFirst.Store(true)
	store := openPullWriteBatchStore(t, hook)
	key := PullKey{Alias: "hub", Repository: "library/node", Reference: "failed"}
	at := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	hook.releaseGate()
	if _, err := store.RecordPull(t.Context(), key, at); err == nil {
		t.Fatal("first pull write unexpectedly succeeded")
	}
	record, err := store.RecordPull(t.Context(), key, at.Add(time.Second))
	if err != nil {
		t.Fatalf("record pull after failed batch: %v", err)
	}
	if record.Count != 1 || !record.LastPullAt.Equal(at.Add(time.Second)) {
		t.Fatalf("failed batch was replayed or successful mutation was lost: %#v", record)
	}
}
