package meta

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
)

func startBlobCatalogRefresh(ctx context.Context, wg *sync.WaitGroup, store *SQLStore, digest string) <-chan error {
	result := make(chan error, 1)
	wg.Go(func() {
		result <- store.refreshRepositoriesForBlob(ctx, digest, time.Now().UTC())
	})
	return result
}

func receiveBlobCatalogRefresh(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("digest catalog refresh did not finish")
		return nil
	}
}

type catalogRefreshGateContext struct {
	context.Context
	entered   chan struct{}
	releaseCh chan struct{}
	once      sync.Once
}

func newCatalogRefreshGateContext(parent context.Context) *catalogRefreshGateContext {
	return &catalogRefreshGateContext{
		Context:   parent,
		entered:   make(chan struct{}),
		releaseCh: make(chan struct{}),
	}
}

func (c *catalogRefreshGateContext) Err() error {
	c.once.Do(func() {
		close(c.entered)
		<-c.releaseCh
	})
	return wrapError(c.Context.Err(), "read test context error")
}

func (c *catalogRefreshGateContext) release() {
	select {
	case <-c.releaseCh:
	default:
		close(c.releaseCh)
	}
}

type blobCatalogRefreshHook struct {
	armed                    atomic.Bool
	listQueries              atomic.Int64
	repositoryUpdates        atomic.Int64
	blobWrites               atomic.Int64
	upstreamUpdateStatements atomic.Int64
	injectedFailures         atomic.Int64
	upstreamAggregates       atomic.Int64
	pauseFirstRefresh        atomic.Bool
	failRefresh              atomic.Int32
	firstListStarted         chan struct{}
	secondListStarted        chan struct{}
	firstRefreshStarted      chan struct{}
	listRelease              chan struct{}
	refreshRelease           chan struct{}
	startOnce                sync.Once
	secondOnce               sync.Once
	refreshStartOnce         sync.Once
	releaseOnce              sync.Once
	refreshReleaseOnce       sync.Once
}

func newBlobCatalogRefreshHook() *blobCatalogRefreshHook {
	return &blobCatalogRefreshHook{
		firstListStarted:    make(chan struct{}),
		secondListStarted:   make(chan struct{}),
		firstRefreshStarted: make(chan struct{}),
		listRelease:         make(chan struct{}),
		refreshRelease:      make(chan struct{}),
	}
}

func (h *blobCatalogRefreshHook) dbHook() dbx.Hook {
	return dbx.HookFuncs{BeforeFunc: h.before, AfterFunc: h.after}
}

func (h *blobCatalogRefreshHook) before(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	if err := h.failUpstreamUpdate(event); err != nil {
		return ctx, err
	}
	if !h.armed.Load() || !isRepositoryBlobDigestList(event) {
		return ctx, nil
	}
	switch h.listQueries.Add(1) {
	case 1:
		h.startOnce.Do(func() { close(h.firstListStarted) })
		select {
		case <-h.listRelease:
			return ctx, nil
		case <-ctx.Done():
			return ctx, wrapError(ctx.Err(), "wait for repository blob query")
		}
	case 2:
		h.secondOnce.Do(func() { close(h.secondListStarted) })
	}
	return ctx, nil
}

func (h *blobCatalogRefreshHook) failUpstreamUpdate(event *dbx.HookEvent) error {
	if !h.armed.Load() || !isCatalogRefreshUpstreamUpdate(event) {
		return nil
	}
	h.upstreamUpdateStatements.Add(1)
	remaining := h.failRefresh.Load()
	if remaining > 0 && h.failRefresh.CompareAndSwap(remaining, remaining-1) {
		h.injectedFailures.Add(1)
		return errBlobCatalogRefreshInjected
	}
	return nil
}

func (h *blobCatalogRefreshHook) after(_ context.Context, event *dbx.HookEvent) {
	h.recordSuccessfulMetadataWrite(event)
	h.recordSuccessfulAggregate(event)
	h.pauseFirstRefreshCommit(event)
}

func (h *blobCatalogRefreshHook) recordSuccessfulMetadataWrite(event *dbx.HookEvent) {
	if !h.armed.Load() || event.Operation != dbx.OperationExec || event.Err != nil {
		return
	}
	if isCatalogRefreshRepositoryUpdate(event) {
		h.repositoryUpdates.Add(1)
	}
	if isBlobMetadataWrite(event.SQL) {
		h.blobWrites.Add(1)
	}
}

func (h *blobCatalogRefreshHook) recordSuccessfulAggregate(event *dbx.HookEvent) {
	if !h.armed.Load() || event.Err != nil || (event.Operation != dbx.OperationQuery && event.Operation != dbx.OperationQueryRow) {
		return
	}
	if isUpstreamAggregateQuery(event.SQL) {
		h.upstreamAggregates.Add(1)
	}
}

func (h *blobCatalogRefreshHook) pauseFirstRefreshCommit(event *dbx.HookEvent) {
	if !h.armed.Load() || event.Operation != dbx.OperationCommitTx || !h.pauseFirstRefresh.CompareAndSwap(true, false) {
		return
	}
	h.refreshStartOnce.Do(func() { close(h.firstRefreshStarted) })
	<-h.refreshRelease
}

func (h *blobCatalogRefreshHook) releaseList() {
	h.releaseOnce.Do(func() { close(h.listRelease) })
}

func (h *blobCatalogRefreshHook) releaseAll() {
	h.releaseList()
	h.releaseRefresh()
}

func (h *blobCatalogRefreshHook) releaseRefresh() {
	h.refreshReleaseOnce.Do(func() { close(h.refreshRelease) })
}

func isRepositoryBlobDigestList(event *dbx.HookEvent) bool {
	if event.Operation != dbx.OperationQuery && event.Operation != dbx.OperationQueryRow {
		return false
	}
	sql := strings.ToLower(event.SQL)
	return strings.Contains(sql, "meta_repo_blobs") &&
		strings.Contains(sql, "where") && strings.Contains(sql, "digest") &&
		!strings.Contains(sql, "join")
}

func waitForBlobCatalogOwner(t *testing.T, ctx *catalogRefreshGateContext, hook *blobCatalogRefreshHook) {
	t.Helper()
	select {
	case <-ctx.entered:
	case <-hook.firstListStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("digest refresh caller did not enter its registration barrier")
	}
}

func waitForBlobCatalogSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("digest refresh did not reach its SQL synchronization point")
	}
}

var (
	errDuplicateDigestRefresh     = errors.New("digest refresh issued a duplicate repository-blob list query")
	errMissingDigestRegistration  = errors.New("second caller did not register with the active digest refresh")
	errBlobCatalogRefreshInjected = errors.New("injected blob catalog refresh failure")
)

func isCatalogRefreshUpstreamUpdate(event *dbx.HookEvent) bool {
	if event.Operation != dbx.OperationExec {
		return false
	}
	sql := strings.ToLower(strings.TrimSpace(event.SQL))
	return strings.HasPrefix(sql, "update") && strings.Contains(sql, "meta_upstreams")
}

func isCatalogRefreshRepositoryUpdate(event *dbx.HookEvent) bool {
	sql := strings.ToLower(strings.TrimSpace(event.SQL))
	return strings.HasPrefix(sql, "update") && strings.Contains(sql, "meta_repositories")
}

func isBlobMetadataWrite(sql string) bool {
	sql = strings.ToLower(strings.TrimSpace(sql))
	return (strings.HasPrefix(sql, "insert into") || strings.HasPrefix(sql, "update")) && strings.Contains(sql, "meta_blobs")
}
