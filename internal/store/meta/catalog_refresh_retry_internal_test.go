package meta

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
)

func TestRepositoryRefreshRetainsFailedSnapshotForLaterHealthyTicket(t *testing.T) {
	const alias = "alias-retry-late"
	hook := newAliasRetryFailureHook()
	store, err := OpenSQLiteWithOptions(t.Context(), DBOptions{
		Path:  filepath.Join(t.TempDir(), "catalog-retry.db"),
		Hooks: []dbx.Hook{hook.dbHook()},
	})
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() {
		hook.releaseSecondFailure()
		if err := store.Close(); err != nil {
			t.Errorf("close metadata store: %v", err)
		}
	})
	hook.armed.Store(true)

	firstKey := PullKey{Alias: alias, Repository: "library/first", Reference: "stable"}
	secondKey := PullKey{Alias: alias, Repository: "library/second", Reference: "stable"}
	thirdKey := PullKey{Alias: alias, Repository: "library/third", Reference: "stable"}
	first := startPullBatchMutation(t.Context(), store, firstKey, time.Now().UTC())
	waitForPullCompletionSignal(t, hook.secondFailureStarted, "second refresh failure")
	second := startPullBatchMutation(t.Context(), store, secondKey, time.Now().UTC().Add(time.Second))
	third := startPullBatchMutation(t.Context(), store, thirdKey, time.Now().UTC().Add(2*time.Second))
	if !waitForCatalogRefreshUsers(t, store, alias, 3) {
		hook.releaseSecondFailure()
		t.Fatalf("late callers did not register all tickets: users=%d", catalogRefreshUsers(store, alias))
	}
	hook.releaseSecondFailure()

	assertFailedSnapshotResults(t, first, second, third)
	if got := hook.refreshAttempts.Load(); got != 3 {
		t.Fatalf("upstream ensure attempts = %d, want two failures and one healthy repository batch", got)
	}
	assertFailedSnapshotRows(t, store, alias, firstKey, secondKey, thirdKey)
	if got := hook.pullWrites.Load(); got != 3 {
		t.Fatalf("canonical pull writes = %d, want one per caller", got)
	}
	assertPullAndCatalogRefreshStatesReleased(t, store)
}

func assertFailedSnapshotResults(t *testing.T, first, second, third <-chan pullBatchMutationResult) {
	t.Helper()
	firstResult := receivePullBatchMutation(t, first)
	if !errors.Is(firstResult.err, errAliasRetryInjected) {
		t.Fatalf("first caller error = %v, want final injected refresh error", firstResult.err)
	}
	for description, result := range map[string]<-chan pullBatchMutationResult{
		"second": second,
		"third":  third,
	} {
		got := receivePullBatchMutation(t, result)
		if got.err != nil || got.record == nil || got.record.Count != 1 {
			t.Fatalf("%s healthy late caller: record=%#v error=%v", description, got.record, got.err)
		}
	}
}

func assertFailedSnapshotRows(t *testing.T, store *SQLStore, alias string, keys ...PullKey) {
	t.Helper()
	assertUpstreamCounts(t, store, alias, 3, 3)
	upstream, err := store.UpstreamByAlias(t.Context(), alias)
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	for _, key := range keys {
		record, err := store.RepositoryByName(t.Context(), upstream.ID, key.Repository)
		if err != nil || record.PullCount != 1 {
			t.Fatalf("repository %q after late repair: record=%#v error=%v", key.Repository, record, err)
		}
	}
}

func TestPostCommitCatalogContextIsDetachedAndBounded(t *testing.T) {
	parent, cancelParent := context.WithCancel(t.Context())
	completion, cancelCompletion := postCommitCatalogContext(parent, 25*time.Millisecond)
	defer cancelCompletion()
	cancelParent()
	if err := completion.Err(); err != nil {
		t.Fatalf("post-commit context inherited caller cancellation: %v", err)
	}
	if _, ok := completion.Deadline(); !ok {
		t.Fatal("post-commit context has no hard deadline")
	}
	select {
	case <-completion.Done():
		if !errors.Is(completion.Err(), context.DeadlineExceeded) {
			t.Fatalf("post-commit context error = %v, want deadline exceeded", completion.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("post-commit context exceeded its bounded deadline")
	}
}

type aliasRetryFailureHook struct {
	armed                atomic.Bool
	refreshAttempts      atomic.Int64
	pullWrites           atomic.Int64
	secondFailureStarted chan struct{}
	secondFailureRelease chan struct{}
	secondFailureOnce    sync.Once
	secondReleaseOnce    sync.Once
}

func newAliasRetryFailureHook() *aliasRetryFailureHook {
	return &aliasRetryFailureHook{
		secondFailureStarted: make(chan struct{}),
		secondFailureRelease: make(chan struct{}),
	}
}

func (h *aliasRetryFailureHook) dbHook() dbx.Hook {
	return dbx.HookFuncs{BeforeFunc: h.before, AfterFunc: h.after}
}

func (h *aliasRetryFailureHook) before(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	if !h.armed.Load() || !isAliasEnsureQuery(event) {
		return ctx, nil
	}
	switch h.refreshAttempts.Add(1) {
	case 1:
		return ctx, errAliasRetryInjected
	case 2:
		h.secondFailureOnce.Do(func() { close(h.secondFailureStarted) })
		<-h.secondFailureRelease
		return ctx, errAliasRetryInjected
	default:
		return ctx, nil
	}
}

func isAliasEnsureQuery(event *dbx.HookEvent) bool {
	if !isCatalogQuery(event) {
		return false
	}
	sql := strings.ToLower(event.SQL)
	return strings.Contains(sql, "meta_upstreams") && strings.Contains(sql, " limit 1") && !strings.Contains(sql, "for update")
}

func (h *aliasRetryFailureHook) after(_ context.Context, event *dbx.HookEvent) {
	if h.armed.Load() && event.Operation == dbx.OperationExec && event.Err == nil && isPullWriteBatchInsert(event.SQL) {
		h.pullWrites.Add(1)
	}
}

func (h *aliasRetryFailureHook) releaseSecondFailure() {
	h.secondReleaseOnce.Do(func() { close(h.secondFailureRelease) })
}

var errAliasRetryInjected = errors.New("injected alias catalog refresh failure")
