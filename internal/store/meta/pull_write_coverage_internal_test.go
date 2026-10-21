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

func TestPullWriteBatchLateCallerDoesNotSucceedAfterMissedFailedRetry(t *testing.T) {
	hook := newPullCoverageHook()
	store := openPullCoverageStore(t, hook)
	hook.failRefresh.Store(true)
	key := PullKey{Alias: "hub", Repository: "library/node", Reference: "missed-failed-retry"}
	at := time.Date(2026, time.October, 16, 13, 0, 0, 0, time.UTC)

	var group sync.WaitGroup
	lateCtx := newPullCoverageContext(t.Context())
	owner := startPullCoverageMutation(t.Context(), &group, store, key, at)
	defer func() {
		hook.releaseFirst()
		close(hook.retryRelease)
		lateCtx.releaseErr()
		group.Wait()
	}()
	waitForPullWriteBatchHook(t, hook.firstStarted)

	late := startPullCoverageMutation(lateCtx, &group, store, key, at.Add(time.Second))
	lateJoined := make(chan struct{})
	followerCtx := &pullBatchJoinedContext{Context: t.Context(), joined: lateJoined}
	follower := startPullCoverageMutation(followerCtx, &group, store, key, at.Add(2*time.Second))
	waitForPullWriteBatchHook(t, lateCtx.joined)
	waitForPullWriteBatchHook(t, lateJoined)
	if !waitForPullWriteBatchUsers(t, store, key.String(), 3) {
		t.Fatal("owner and both late callers did not register with the batch")
	}
	lateCtx.armPause()
	hook.retryPause = lateCtx.paused
	hook.releaseFirst()

	select {
	case <-lateCtx.paused:
	case <-time.After(5 * time.Second):
		t.Fatal("late caller did not pause after receiving the first batch error")
	}
	ownerResult := receivePullCoverageMutation(t, owner)
	if !errors.Is(ownerResult.err, errPullCoverageInjected) {
		t.Fatalf("owner error = %v, want first refresh failure", ownerResult.err)
	}
	followerResult := receivePullCoverageMutation(t, follower)
	if !errors.Is(followerResult.err, errPullCoverageInjected) {
		t.Fatalf("retry follower error = %v, want second upsert failure", followerResult.err)
	}

	lateCtx.releaseErr()
	lateResult := receivePullCoverageMutation(t, late)
	assertPullCoverageState(t, lateResult, hook, store, key)
}

func assertPullCoverageState(t *testing.T, lateResult pullCoverageMutationResult, hook *pullCoverageHook, store *SQLStore, key PullKey) {
	t.Helper()
	if lateResult.err == nil {
		t.Errorf("late caller falsely succeeded after its retry failed: %#v", lateResult.record)
	}
	if got := hook.attempts.Load(); got != 2 {
		t.Errorf("pull upsert attempts = %d, want two; a missed result must not start an empty third flush", got)
	}
	if got := hook.writes.Load(); got != 1 {
		t.Errorf("committed pull upserts = %d, want only the owner's first write", got)
	}

	stored, ok, err := store.Pull(t.Context(), key)
	if err != nil || !ok || stored.Count != 1 {
		t.Fatalf("canonical row after failed retry: record=%#v found=%v error=%v", stored, ok, err)
	}
	pulls, err := store.ListPulls(t.Context())
	if err != nil {
		t.Fatalf("list pull rows: %v", err)
	}
	if pulls.Len() != 1 || pulls.Values()[0].Key != key.String() {
		t.Fatalf("unexpected pull rows after missed retry: %#v", pulls.Values())
	}
}

func openPullCoverageStore(t *testing.T, hook *pullCoverageHook) *SQLStore {
	t.Helper()
	store, err := OpenSQLiteWithOptions(t.Context(), DBOptions{
		Path:  filepath.Join(t.TempDir(), "pull-coverage.db"),
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

type pullCoverageMutationResult struct {
	record *PullRecord
	err    error
}

func startPullCoverageMutation(ctx context.Context, wg *sync.WaitGroup, store *SQLStore, key PullKey, at time.Time) <-chan pullCoverageMutationResult {
	result := make(chan pullCoverageMutationResult, 1)
	wg.Go(func() {
		record, err := store.RecordPull(ctx, key, at)
		result <- pullCoverageMutationResult{record: record, err: err}
	})
	return result
}

func receivePullCoverageMutation(t *testing.T, result <-chan pullCoverageMutationResult) pullCoverageMutationResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("pull mutation did not finish")
		return pullCoverageMutationResult{}
	}
}

type pullCoverageContext struct {
	context.Context
	joined    chan struct{}
	paused    chan struct{}
	release   chan struct{}
	joinOnce  sync.Once
	pauseOnce sync.Once
	arm       atomic.Bool
}

func newPullCoverageContext(parent context.Context) *pullCoverageContext {
	return &pullCoverageContext{
		Context: parent,
		joined:  make(chan struct{}),
		paused:  make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (c *pullCoverageContext) Done() <-chan struct{} {
	c.joinOnce.Do(func() { close(c.joined) })
	return c.Context.Done()
}

func (c *pullCoverageContext) Err() error {
	if c.arm.Load() {
		c.pauseOnce.Do(func() {
			close(c.paused)
			<-c.release
		})
	}
	err := c.Context.Err()
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

func (c *pullCoverageContext) armPause() {
	c.arm.Store(true)
}

func (c *pullCoverageContext) releaseErr() {
	select {
	case <-c.release:
	default:
		close(c.release)
	}
}

type pullCoverageHook struct {
	firstStarted chan struct{}
	firstRelease chan struct{}
	startOnce    sync.Once
	releaseOnce  sync.Once
	attempts     atomic.Int64
	writes       atomic.Int64
	failRefresh  atomic.Bool
	retryPause   <-chan struct{}
	retryRelease chan struct{}
}

func newPullCoverageHook() *pullCoverageHook {
	return &pullCoverageHook{
		firstStarted: make(chan struct{}),
		firstRelease: make(chan struct{}),
		retryRelease: make(chan struct{}),
	}
}

func (h *pullCoverageHook) dbHook() dbx.Hook {
	return dbx.HookFuncs{BeforeFunc: h.before, AfterFunc: h.after}
}

func (h *pullCoverageHook) before(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	sql := strings.ToLower(event.SQL)
	if err := h.refreshError(sql); err != nil {
		return ctx, err
	}
	if event.Operation != dbx.OperationExec || !isPullWriteBatchInsert(event.SQL) {
		return ctx, nil
	}
	switch h.attempts.Add(1) {
	case 1:
		h.startOnce.Do(func() { close(h.firstStarted) })
		select {
		case <-h.firstRelease:
			return ctx, nil
		case <-ctx.Done():
			return ctx, fmt.Errorf("wait for first pull write: %w", ctx.Err())
		}
	case 2:
		if err := h.waitForRetry(ctx); err != nil {
			return ctx, err
		}
		return ctx, errPullCoverageInjected
	default:
		return ctx, nil
	}
}

func (h *pullCoverageHook) refreshError(sql string) error {
	if strings.Contains(sql, "meta_upstreams") && h.failRefresh.CompareAndSwap(true, false) {
		return errPullCoverageInjected
	}
	return nil
}

func (h *pullCoverageHook) waitForRetry(ctx context.Context) error {
	if h.retryPause == nil {
		return nil
	}
	select {
	case <-h.retryPause:
		return nil
	case <-h.retryRelease:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for pull write retry: %w", ctx.Err())
	}
}

func (h *pullCoverageHook) after(_ context.Context, event *dbx.HookEvent) {
	if event.Operation == dbx.OperationExec && event.Err == nil && isPullWriteBatchInsert(event.SQL) {
		h.writes.Add(1)
	}
}

func (h *pullCoverageHook) releaseFirst() {
	h.releaseOnce.Do(func() { close(h.firstRelease) })
}

var errPullCoverageInjected = errors.New("injected pull coverage failure")
