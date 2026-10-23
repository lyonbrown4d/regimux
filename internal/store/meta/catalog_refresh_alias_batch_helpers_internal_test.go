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

func openAliasBatchRefreshStore(t *testing.T, hook *aliasBatchRefreshHook) *SQLStore {
	t.Helper()
	store, err := OpenSQLiteWithOptions(t.Context(), DBOptions{
		Path:  filepath.Join(t.TempDir(), "alias-refresh.db"),
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

func holdAliasCatalogGate(t *testing.T, store *SQLStore) func() {
	t.Helper()
	if err := store.catalogMu.Acquire(t.Context(), 1); err != nil {
		t.Fatalf("hold catalog gate: %v", err)
	}
	var once sync.Once
	release := func() { once.Do(func() { store.catalogMu.Release(1) }) }
	t.Cleanup(release)
	return release
}

func waitForCatalogRefreshUsers(t *testing.T, store *SQLStore, alias string, want int) bool {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if catalogRefreshUsers(store, alias) >= want {
			return true
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			return false
		}
	}
}

func catalogRefreshUsers(store *SQLStore, alias string) int {
	store.catalogRefreshMu.Lock()
	defer store.catalogRefreshMu.Unlock()
	users := 0
	for key, state := range store.catalogRefreshes {
		if key.Alias == alias {
			users += state.users
		}
	}
	return users
}

func receiveAliasRefresh(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("alias refresh did not finish")
		return nil
	}
}

func assertAliasRefreshResults(t *testing.T, results <-chan error, count int, operation string) {
	t.Helper()
	for range count {
		if err := receiveAliasRefresh(t, results); err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
	}
}

func assertUpstreamCounts(t *testing.T, store *SQLStore, alias string, repositories, pulls int64) {
	t.Helper()
	upstream, err := store.UpstreamByAlias(t.Context(), alias)
	if err != nil {
		t.Fatalf("read refreshed upstream: %v", err)
	}
	if upstream.RepositoryCount != repositories || upstream.PullCount != pulls {
		t.Fatalf("upstream counts = repositories:%d pulls:%d, want %d/%d", upstream.RepositoryCount, upstream.PullCount, repositories, pulls)
	}
}

func assertUpstreamBlobCounts(t *testing.T, store *SQLStore, alias string, repositories, bytes int64) {
	t.Helper()
	upstream, err := store.UpstreamByAlias(t.Context(), alias)
	if err != nil {
		t.Fatalf("read refreshed upstream: %v", err)
	}
	if upstream.RepositoryCount != repositories || upstream.BlobBytes != bytes {
		t.Fatalf("upstream catalog = repositories:%d bytes:%d, want %d/%d", upstream.RepositoryCount, upstream.BlobBytes, repositories, bytes)
	}
}

type aliasBatchRefreshHook struct {
	armed                         atomic.Bool
	upstreamAggregates            atomic.Int64
	pauseFirstCommit              atomic.Bool
	firstCommitStarted            chan struct{}
	commitRelease                 chan struct{}
	releaseOnce                   sync.Once
	pauseAndFailFirstUpstreamRead atomic.Bool
	firstUpstreamReadStarted      chan struct{}
	upstreamReadRelease           chan struct{}
	upstreamReadReleaseOnce       sync.Once
}

func newAliasBatchRefreshHook() *aliasBatchRefreshHook {
	return &aliasBatchRefreshHook{
		firstCommitStarted:       make(chan struct{}),
		commitRelease:            make(chan struct{}),
		firstUpstreamReadStarted: make(chan struct{}),
		upstreamReadRelease:      make(chan struct{}),
	}
}

func (h *aliasBatchRefreshHook) dbHook() dbx.Hook {
	return dbx.HookFuncs{BeforeFunc: h.before, AfterFunc: h.after}
}

func (h *aliasBatchRefreshHook) before(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	if !h.armed.Load() {
		return ctx, nil
	}
	if h.pauseAtUpstreamRead(event) {
		close(h.firstUpstreamReadStarted)
		<-h.upstreamReadRelease
		return ctx, errAliasBatchInjected
	}
	if !isCatalogQuery(event) {
		return ctx, nil
	}
	if isUpstreamAggregateQuery(event.SQL) {
		h.upstreamAggregates.Add(1)
	}
	return ctx, nil
}

func (h *aliasBatchRefreshHook) pauseAtUpstreamRead(event *dbx.HookEvent) bool {
	return isCatalogQuery(event) && strings.Contains(strings.ToLower(event.SQL), "meta_upstreams") &&
		h.pauseAndFailFirstUpstreamRead.CompareAndSwap(true, false)
}

func isCatalogQuery(event *dbx.HookEvent) bool {
	return event.Operation == dbx.OperationQuery || event.Operation == dbx.OperationQueryRow
}

func isUpstreamAggregateQuery(statement string) bool {
	statement = strings.ToLower(statement)
	return strings.Contains(statement, "meta_repositories") &&
		strings.Contains(statement, "count(") && strings.Contains(statement, "sum(")
}

func (h *aliasBatchRefreshHook) after(_ context.Context, event *dbx.HookEvent) {
	if !h.armed.Load() || event.Operation != dbx.OperationCommitTx || !h.pauseFirstCommit.CompareAndSwap(true, false) {
		return
	}
	close(h.firstCommitStarted)
	<-h.commitRelease
}

func (h *aliasBatchRefreshHook) releaseFirstCommit() {
	h.releaseOnce.Do(func() { close(h.commitRelease) })
}

func (h *aliasBatchRefreshHook) releaseFirstUpstreamRead() {
	h.upstreamReadReleaseOnce.Do(func() { close(h.upstreamReadRelease) })
}

var errAliasBatchInjected = errors.New("injected alias batch failure")
