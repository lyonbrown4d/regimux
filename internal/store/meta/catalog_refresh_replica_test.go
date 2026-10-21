package meta_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestSQLStoreCatalogRefreshSerializesAcrossStores(t *testing.T) {
	driver, dsn, ok := catalogConcurrencyConfig()
	if !ok {
		t.Skip("set REGIMUX_META_CONCURRENCY_DRIVER and REGIMUX_META_CONCURRENCY_DSN for shared-database concurrency coverage")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	hooks := newCatalogRaceHooks()
	storeA := openCatalogRaceStore(ctx, t, driver, dsn, hooks.owner())
	storeB := openCatalogRaceStore(ctx, t, driver, dsn, hooks.follower())
	defer hooks.releaseOwner()

	alias := fmt.Sprintf("catalog-race-%d", time.Now().UnixNano())
	first := meta.PullKey{Alias: alias, Repository: "library/first", Reference: "stable"}
	second := meta.PullKey{Alias: alias, Repository: "library/second", Reference: "stable"}
	startCatalogRace(ctx, t, hooks, storeA, storeB, first, second)
	assertCatalogRaceResult(ctx, t, storeA, alias, first, second, hooks)
}

func catalogConcurrencyConfig() (string, string, bool) {
	driver := strings.ToLower(os.Getenv("REGIMUX_META_CONCURRENCY_DRIVER"))
	dsn := os.Getenv("REGIMUX_META_CONCURRENCY_DSN")
	return driver, dsn, (driver == "postgres" || driver == "mysql") && dsn != ""
}

func openCatalogRaceStore(ctx context.Context, t *testing.T, driver, dsn string, hook dbx.Hook) *meta.SQLStore {
	t.Helper()
	store, err := meta.OpenDBWithOptions(ctx, meta.DBOptions{Driver: driver, DSN: dsn, Hooks: []dbx.Hook{hook}})
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

func startCatalogRace(
	ctx context.Context,
	t *testing.T,
	hooks *catalogRaceHooks,
	storeA, storeB *meta.SQLStore,
	first, second meta.PullKey,
) {
	t.Helper()
	startedAt := time.Now().UTC()
	firstDone := make(chan error, 1)
	hooks.pauseOwner.Store(true)
	go recordCatalogPull(ctx, storeA, first, startedAt, firstDone)
	waitForCatalogSignal(t, hooks.ownerPaused, "first refresh to reach its upstream write")
	secondDone := make(chan error, 1)
	go recordCatalogPull(ctx, storeB, second, startedAt.Add(time.Second), secondDone)
	waitForCatalogSignal(t, hooks.pullCommitted, "second canonical pull write")
	waitForCatalogSignal(t, hooks.refreshStarted, "second refresh to start")
	hooks.updatedBeforeRelease = waitForSecondCatalogRefresh(ctx, t, hooks)
	hooks.releaseOwner()
	if err := <-firstDone; err != nil {
		t.Fatalf("first pull: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second pull: %v", err)
	}
}

func recordCatalogPull(ctx context.Context, store *meta.SQLStore, key meta.PullKey, at time.Time, result chan<- error) {
	_, err := store.RecordPull(ctx, key, at)
	result <- err
}

func waitForSecondCatalogRefresh(ctx context.Context, t *testing.T, hooks *catalogRaceHooks) bool {
	t.Helper()
	select {
	case <-hooks.upstreamUpdated:
		return true
	case <-hooks.lockAttempted:
		select {
		case <-hooks.upstreamUpdated:
			return true
		case <-time.After(100 * time.Millisecond):
			return false
		}
	case <-ctx.Done():
		t.Fatalf("second refresh did not reach the upstream lock: %v", ctx.Err())
		return false
	}
}

func assertCatalogRaceResult(
	ctx context.Context,
	t *testing.T,
	store *meta.SQLStore,
	alias string,
	first, second meta.PullKey,
	hooks *catalogRaceHooks,
) {
	t.Helper()
	upstream, err := store.UpstreamByAlias(ctx, alias)
	if err != nil {
		t.Fatalf("read refreshed upstream: %v", err)
	}
	if upstream.PullCount != 2 {
		t.Fatalf("upstream pull count = %d, want 2", upstream.PullCount)
	}
	if hooks.updatedBeforeRelease {
		t.Fatal("second store refreshed upstream before the first lock holder released it")
	}
	assertRepositoryPullCount(ctx, t, store, upstream.ID, first.Repository, 1)
	assertRepositoryPullCount(ctx, t, store, upstream.ID, second.Repository, 1)
}

func assertRepositoryPullCount(ctx context.Context, t *testing.T, store *meta.SQLStore, upstreamID int64, name string, want int64) {
	t.Helper()
	repo, err := store.RepositoryByName(ctx, upstreamID, name)
	if err != nil {
		t.Fatalf("read repository %q: %v", name, err)
	}
	if repo.PullCount != want {
		t.Fatalf("repository %q pull count = %d, want %d", name, repo.PullCount, want)
	}
}

func waitForCatalogSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

type catalogRaceHooks struct {
	pauseOwner           atomic.Bool
	ownerPaused          chan struct{}
	releaseOwnerCh       chan struct{}
	pullCommitted        chan struct{}
	refreshStarted       chan struct{}
	lockAttempted        chan struct{}
	upstreamUpdated      chan struct{}
	updatedBeforeRelease bool
	once                 sync.Once
	releaseOnce          sync.Once
	commitOnce           sync.Once
	refreshOnce          sync.Once
	lockOnce             sync.Once
	updateOnce           sync.Once
}

func newCatalogRaceHooks() *catalogRaceHooks {
	return &catalogRaceHooks{
		ownerPaused:     make(chan struct{}),
		releaseOwnerCh:  make(chan struct{}),
		pullCommitted:   make(chan struct{}),
		refreshStarted:  make(chan struct{}),
		lockAttempted:   make(chan struct{}),
		upstreamUpdated: make(chan struct{}),
	}
}

func (h *catalogRaceHooks) owner() dbx.Hook {
	return dbx.HookFuncs{BeforeFunc: h.beforeOwnerUpdate}
}

func (h *catalogRaceHooks) beforeOwnerUpdate(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	if h.pauseOwner.Load() && event.Operation == dbx.OperationExec && isCatalogUpdate(event.SQL, "meta_upstreams") {
		h.once.Do(func() { close(h.ownerPaused) })
		select {
		case <-h.releaseOwnerCh:
		case <-ctx.Done():
			return ctx, fmt.Errorf("wait for catalog owner release: %w", ctx.Err())
		}
	}
	return ctx, nil
}

func (h *catalogRaceHooks) follower() dbx.Hook {
	return dbx.HookFuncs{BeforeFunc: h.beforeFollowerQuery, AfterFunc: h.afterFollowerExec}
}

func (h *catalogRaceHooks) beforeFollowerQuery(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	sql := strings.ToLower(event.SQL)
	if (event.Operation == dbx.OperationQuery || event.Operation == dbx.OperationQueryRow) && strings.Contains(sql, "meta_upstreams") {
		h.refreshOnce.Do(func() { close(h.refreshStarted) })
	}
	if strings.Contains(sql, "for update") && strings.Contains(sql, "meta_upstreams") {
		h.lockOnce.Do(func() { close(h.lockAttempted) })
	}
	return ctx, nil
}

func (h *catalogRaceHooks) afterFollowerExec(_ context.Context, event *dbx.HookEvent) {
	if event.Operation != dbx.OperationExec {
		return
	}
	if isPullInsert(event.SQL) {
		h.commitOnce.Do(func() { close(h.pullCommitted) })
	}
	if isCatalogUpdate(event.SQL, "meta_upstreams") {
		h.updateOnce.Do(func() { close(h.upstreamUpdated) })
	}
}

func (h *catalogRaceHooks) releaseOwner() {
	h.releaseOnce.Do(func() { close(h.releaseOwnerCh) })
}

func isCatalogUpdate(statement, table string) bool {
	sql := strings.ToLower(strings.TrimSpace(statement))
	return strings.HasPrefix(sql, "update") && strings.Contains(sql, table)
}

func isPullInsert(statement string) bool {
	sql := strings.ToLower(strings.TrimSpace(statement))
	return strings.HasPrefix(sql, "insert") && strings.Contains(sql, "meta_pulls")
}
