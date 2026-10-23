package meta

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRefreshRepositoriesForBlobCoalescesConcurrentDigestRefreshes(t *testing.T) {
	hook := newBlobCatalogRefreshHook()
	store := openBlobCatalogRefreshStore(t, hook)
	digest := seedBlobCatalogRefresh(t, store)

	hook.armed.Store(true)
	ctxA := newCatalogRefreshGateContext(t.Context())
	ctxB := newCatalogRefreshGateContext(t.Context())
	var group sync.WaitGroup
	resultA := startBlobCatalogRefresh(ctxA, &group, store, digest)
	waitForBlobCatalogOwner(t, ctxA, hook)
	resultB := startBlobCatalogRefresh(ctxB, &group, store, digest)
	defer func() {
		ctxA.release()
		ctxB.release()
		hook.releaseAll()
		group.Wait()
	}()

	if err := waitForCatalogBatchRegistration(t, store, catalogRefreshKey{Digest: digest}, hook.secondListStarted); err != nil {
		t.Fatal(err)
	}
	ctxA.release()
	ctxB.release()
	hook.releaseList()

	if err := receiveBlobCatalogRefresh(t, resultA); err != nil {
		t.Fatalf("first digest refresh: %v", err)
	}
	if err := receiveBlobCatalogRefresh(t, resultB); err != nil {
		t.Fatalf("second digest refresh: %v", err)
	}
	if got := hook.listQueries.Load(); got != 1 {
		t.Fatalf("repository-blob list queries = %d, want one coalesced digest refresh", got)
	}
	store.catalogRefreshMu.Lock()
	active := len(store.catalogRefreshes)
	store.catalogRefreshMu.Unlock()
	if active != 0 {
		t.Fatalf("completed digest refresh retained %d catalog refresh states", active)
	}
}

func TestRefreshRepositoriesForBlobLateTicketGetsAnotherSweep(t *testing.T) {
	hook := newBlobCatalogRefreshHook()
	store := openBlobCatalogRefreshStore(t, hook)
	digest := seedBlobCatalogRefresh(t, store)
	hook.armed.Store(true)
	hook.pauseFirstRefresh.Store(true)
	hook.releaseList()

	first := make(chan error, 1)
	go func() {
		first <- store.refreshRepositoriesForBlob(t.Context(), digest, time.Now().UTC())
	}()
	waitForBlobCatalogSignal(t, hook.firstRefreshStarted)

	second := make(chan error, 1)
	go func() {
		_, err := store.UpsertBlob(t.Context(), BlobRecord{Digest: digest, Size: 200})
		second <- err
	}()
	if !waitForCatalogRefreshKeyUsers(t, store, catalogRefreshKey{Digest: digest}, 2) {
		hook.releaseRefresh()
		t.Fatal("late size mutation did not register its digest ticket")
	}
	hook.releaseRefresh()
	if err := receiveBlobCatalogRefresh(t, first); err != nil {
		t.Fatalf("first digest refresh: %v", err)
	}
	if err := receiveBlobCatalogRefresh(t, second); err != nil {
		t.Fatalf("late size mutation: %v", err)
	}
	if got := hook.listQueries.Load(); got != 2 {
		t.Fatalf("digest sweep count = %d, want first plus one late-ticket sweep", got)
	}
	assertBlobCatalogSize(t, store, digest, 200)
	assertNoCatalogRefreshStates(t, store)
}

func TestRefreshRepositoriesForBlobFailureCanBeRetried(t *testing.T) {
	hook := newBlobCatalogRefreshHook()
	store := openBlobCatalogRefreshStore(t, hook)
	digest := seedBlobCatalogRefresh(t, store)
	hook.armed.Store(true)
	hook.failRefresh.Store(2)
	hook.releaseList()

	if _, err := store.UpsertBlob(t.Context(), BlobRecord{Digest: digest, Size: 200}); !errors.Is(err, errBlobCatalogRefreshInjected) {
		t.Fatalf("failed blob refresh error = %v, want injected failure", err)
	}
	if got := hook.upstreamUpdateStatements.Load(); got != 2 {
		t.Fatalf("failed refresh upstream updates = %d, want two exhausted row-lock attempts", got)
	}
	if got := hook.injectedFailures.Load(); got != 2 {
		t.Fatalf("injected refresh failures = %d, want two exhausted failures", got)
	}
	if got := hook.upstreamAggregates.Load(); got != 0 {
		t.Fatalf("upstream aggregate queries after exhausted refresh = %d, want zero", got)
	}
	assertNoCatalogRefreshStates(t, store)
	if _, err := store.UpsertBlob(t.Context(), BlobRecord{Digest: digest, Size: 200}); err != nil {
		t.Fatalf("retry same-size blob upsert: %v", err)
	}
	if got := hook.upstreamUpdateStatements.Load(); got != 4 {
		t.Fatalf("total upstream updates = %d, want two failed locks and explicit repair lock+aggregate write", got)
	}
	if got := hook.blobWrites.Load(); got != 2 {
		t.Fatalf("canonical blob writes = %d, want initial write and explicit repair", got)
	}
	if got := hook.repositoryUpdates.Load(); got != 2 {
		t.Fatalf("repository aggregate updates after explicit repair = %d, want one per linked repository", got)
	}
	if got := hook.upstreamAggregates.Load(); got != 1 {
		t.Fatalf("upstream aggregate queries after explicit repair = %d, want one", got)
	}
	assertBlobUpstreamBytes(t, store, 400)
	assertBlobCatalogSize(t, store, digest, 200)
	assertNoCatalogRefreshStates(t, store)
}

func TestRefreshRepositoriesForBlobRetriesTransientFailureAutomatically(t *testing.T) {
	hook := newBlobCatalogRefreshHook()
	store := openBlobCatalogRefreshStore(t, hook)
	digest := seedBlobCatalogRefresh(t, store)
	hook.armed.Store(true)
	hook.failRefresh.Store(1)
	hook.releaseList()

	record, err := store.UpsertBlob(t.Context(), BlobRecord{Digest: digest, Size: 200})
	if err != nil {
		t.Fatalf("upsert blob after transient catalog failure: %v", err)
	}
	assertAutomaticBlobCatalogRecovery(t, hook, store, digest, record)
}

func assertAutomaticBlobCatalogRecovery(t *testing.T, hook *blobCatalogRefreshHook, store *SQLStore, digest string, record *BlobRecord) {
	t.Helper()
	assertAutomaticBlobWrite(t, store, digest, record)
	if got := hook.upstreamUpdateStatements.Load(); got != 3 {
		t.Fatalf("upstream update statements = %d, want failed lock plus retry lock and aggregate write", got)
	}
	if got := hook.injectedFailures.Load(); got != 1 {
		t.Fatalf("injected refresh failures = %d, want one", got)
	}
	if got := hook.blobWrites.Load(); got != 1 {
		t.Fatalf("canonical blob writes = %d, want exactly one", got)
	}
	if got := hook.repositoryUpdates.Load(); got != 2 {
		t.Fatalf("repository aggregate updates = %d, want one per linked repository", got)
	}
	if got := hook.upstreamAggregates.Load(); got != 1 {
		t.Fatalf("upstream aggregate queries = %d, want one successful refresh", got)
	}
	assertBlobUpstreamBytes(t, store, 400)
	assertBlobCatalogSize(t, store, digest, 200)
	assertNoCatalogRefreshStates(t, store)
}

func assertAutomaticBlobWrite(t *testing.T, store *SQLStore, digest string, record *BlobRecord) {
	t.Helper()
	if record == nil || record.Size != 200 {
		t.Fatalf("upserted blob = %#v, want size 200", record)
	}
	stored, found, err := store.Blob(t.Context(), BlobKey{Digest: digest})
	if err != nil || !found || stored.Size != 200 {
		t.Fatalf("canonical blob after automatic retry: record=%#v found=%v error=%v", stored, found, err)
	}
}

func assertBlobUpstreamBytes(t *testing.T, store *SQLStore, want int64) {
	t.Helper()
	upstream, err := store.UpstreamByAlias(t.Context(), "hub")
	if err != nil {
		t.Fatalf("read upstream after blob catalog refresh: %v", err)
	}
	if upstream.BlobBytes != want {
		t.Fatalf("upstream blob bytes = %d, want %d", upstream.BlobBytes, want)
	}
}

func TestRefreshRepositoriesForBlobCancellationReleasesBatch(t *testing.T) {
	hook := newBlobCatalogRefreshHook()
	store := openBlobCatalogRefreshStore(t, hook)
	digest := seedBlobCatalogRefresh(t, store)
	if err := store.catalogMu.Acquire(t.Context(), 1); err != nil {
		t.Fatalf("hold catalog gate: %v", err)
	}
	defer store.catalogMu.Release(1)

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		result <- store.refreshRepositoriesForBlob(ctx, digest, time.Now().UTC())
	}()
	if !waitForCatalogRefreshKeyUsers(t, store, catalogRefreshKey{Digest: digest}, 1) {
		cancel()
		t.Fatal("digest refresh did not register before cancellation")
	}
	cancel()
	if err := receiveBlobCatalogRefresh(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled digest refresh error = %v, want context canceled", err)
	}
	if !waitForNoCatalogRefreshStates(t, store) {
		assertNoCatalogRefreshStates(t, store)
	}
}

func TestTouchBlobDoesNotRefreshCatalog(t *testing.T) {
	hook := newBlobCatalogRefreshHook()
	store := openBlobCatalogRefreshStore(t, hook)
	digest := seedBlobCatalogRefresh(t, store)
	before := time.Now().UTC().Add(time.Minute)
	hook.armed.Store(true)
	hook.releaseList()
	if err := store.TouchBlob(t.Context(), BlobKey{Digest: digest}, before); err != nil {
		t.Fatalf("touch blob: %v", err)
	}
	if got := hook.repositoryUpdates.Load(); got != 0 {
		t.Fatalf("repository catalog updates after blob-only touch = %d, want zero", got)
	}
	blob, found, err := store.Blob(t.Context(), BlobKey{Digest: digest})
	if err != nil || !found {
		t.Fatalf("read touched blob: found=%v error=%v", found, err)
	}
	if !blob.LastAccessAt.Equal(before) {
		t.Fatalf("blob last access = %s, want %s", blob.LastAccessAt, before)
	}
}
