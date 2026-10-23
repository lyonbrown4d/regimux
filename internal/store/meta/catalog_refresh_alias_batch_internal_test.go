package meta

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRepositoryRefreshBatchesDistinctRepositoriesByAlias(t *testing.T) {
	const repositoryCount = 6
	alias := "alias-batch"
	hook := newAliasBatchRefreshHook()
	store := openAliasBatchRefreshStore(t, hook)

	releaseGate := holdAliasCatalogGate(t, store)
	hook.armed.Store(true)
	var group sync.WaitGroup
	results := make(chan error, repositoryCount)
	for index := range repositoryCount {
		group.Go(func() {
			_, err := store.RecordPull(t.Context(), PullKey{
				Alias: alias, Repository: fmt.Sprintf("library/repo-%02d", index), Reference: "stable",
			}, time.Now().UTC())
			results <- err
		})
	}
	defer func() {
		releaseGate()
		group.Wait()
	}()

	if !waitForCatalogRefreshUsers(t, store, alias, repositoryCount) {
		t.Fatalf("only %d distinct repository refreshes registered", catalogRefreshUsers(store, alias))
	}
	releaseGate()

	assertAliasRefreshResults(t, results, repositoryCount, "record pull")
	group.Wait()

	if got := hook.upstreamAggregates.Load(); got != 1 {
		t.Fatalf("upstream aggregate queries = %d, want one alias batch", got)
	}
	assertUpstreamCounts(t, store, alias, repositoryCount, repositoryCount)
	assertNoCatalogRefreshStates(t, store)
}

func TestDigestRefreshBatchesAllRepositoriesForAlias(t *testing.T) {
	const repositoryCount = 6
	alias := "digest-alias-batch"
	digest := "sha256:" + strings.Repeat("b", 64)
	hook := newAliasBatchRefreshHook()
	store := openAliasBatchRefreshStore(t, hook)
	if _, err := store.UpsertBlob(t.Context(), BlobRecord{Digest: digest, Size: 100}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	for index := range repositoryCount {
		_, err := store.UpsertRepoBlob(t.Context(), RepoBlobRecord{
			Alias: alias, Repository: fmt.Sprintf("library/repo-%02d", index), Digest: digest,
		})
		if err != nil {
			t.Fatalf("seed repository blob %d: %v", index, err)
		}
	}
	releaseGate := holdAliasCatalogGate(t, store)
	hook.armed.Store(true)
	result := make(chan error, 1)
	var group sync.WaitGroup
	group.Go(func() {
		result <- store.refreshRepositoriesForBlob(t.Context(), digest, time.Now().UTC())
	})
	defer func() {
		releaseGate()
		group.Wait()
	}()
	if !waitForCatalogRefreshUsers(t, store, alias, 1) {
		t.Fatal("digest refresh did not register its alias batch")
	}
	releaseGate()
	if err := receiveAliasRefresh(t, result); err != nil {
		t.Fatalf("refresh digest repositories: %v", err)
	}
	if got := hook.upstreamAggregates.Load(); got != 1 {
		t.Fatalf("upstream aggregate queries for digest sweep = %d, want one alias batch", got)
	}
	assertUpstreamBlobCounts(t, store, alias, repositoryCount, repositoryCount*100)
	assertNoCatalogRefreshStates(t, store)
}

func TestRepositoryRefreshLateTicketGetsAnotherAliasBatch(t *testing.T) {
	alias := "alias-late-ticket"
	hook := newAliasBatchRefreshHook()
	store := openAliasBatchRefreshStore(t, hook)
	var group sync.WaitGroup
	t.Cleanup(func() {
		hook.releaseFirstCommit()
		group.Wait()
	})
	hook.armed.Store(true)
	hook.pauseFirstCommit.Store(true)

	first := make(chan error, 1)
	group.Go(func() {
		_, err := store.RecordPull(t.Context(), PullKey{Alias: alias, Repository: "library/first", Reference: "stable"}, time.Now().UTC())
		first <- err
	})
	select {
	case <-hook.firstCommitStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first repository refresh did not commit")
	}

	second := make(chan error, 1)
	group.Go(func() {
		_, err := store.RecordPull(t.Context(), PullKey{Alias: alias, Repository: "library/second", Reference: "stable"}, time.Now().UTC())
		second <- err
	})
	if !waitForCatalogRefreshUsers(t, store, alias, 2) {
		hook.releaseFirstCommit()
		t.Fatal("late repository mutation did not register its alias ticket")
	}
	hook.releaseFirstCommit()
	if err := receiveAliasRefresh(t, first); err != nil {
		t.Fatalf("first repository pull: %v", err)
	}
	if err := receiveAliasRefresh(t, second); err != nil {
		t.Fatalf("late repository pull: %v", err)
	}
	if got := hook.upstreamAggregates.Load(); got != 2 {
		t.Fatalf("upstream aggregate queries = %d, want one first wave and one late-ticket wave", got)
	}
	assertUpstreamCounts(t, store, alias, 2, 2)
	assertNoCatalogRefreshStates(t, store)
}

func TestRepositoryRefreshLateTicketRetriesAfterEarlierBatchFailure(t *testing.T) {
	alias := "alias-late-failure"
	hook := newAliasBatchRefreshHook()
	store := openAliasBatchRefreshStore(t, hook)
	var group sync.WaitGroup
	t.Cleanup(func() {
		hook.releaseFirstUpstreamRead()
		group.Wait()
	})
	hook.armed.Store(true)
	hook.pauseAndFailFirstUpstreamRead.Store(true)

	first := make(chan error, 1)
	group.Go(func() {
		_, err := store.RecordPull(t.Context(), PullKey{Alias: alias, Repository: "library/first", Reference: "stable"}, time.Now().UTC())
		first <- err
	})
	select {
	case <-hook.firstUpstreamReadStarted:
	case <-time.After(5 * time.Second):
		hook.releaseFirstUpstreamRead()
		t.Fatal("first alias batch did not reach upstream ensure")
	}

	second := make(chan error, 1)
	group.Go(func() {
		_, err := store.RecordPull(t.Context(), PullKey{Alias: alias, Repository: "library/second", Reference: "stable"}, time.Now().UTC())
		second <- err
	})
	if !waitForCatalogRefreshUsers(t, store, alias, 2) {
		hook.releaseFirstUpstreamRead()
		t.Fatal("late repository mutation did not register its alias ticket")
	}
	hook.releaseFirstUpstreamRead()
	if err := receiveAliasRefresh(t, first); err != nil {
		t.Fatalf("first repository pull did not recover from injected batch failure: %v", err)
	}
	if err := receiveAliasRefresh(t, second); err != nil {
		t.Fatalf("late repository pull did not retry its pending batch: %v", err)
	}
	if got := hook.upstreamAggregates.Load(); got != 1 {
		t.Fatalf("upstream aggregate queries = %d, want one successful retry aggregate", got)
	}
	assertAliasLateFailureCounts(t, store, alias)
	assertNoCatalogRefreshStates(t, store)
}

func assertAliasLateFailureCounts(t *testing.T, store *SQLStore, alias string) {
	t.Helper()
	upstream, err := store.UpstreamByAlias(t.Context(), alias)
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	repo, err := store.RepositoryByName(t.Context(), upstream.ID, "library/second")
	if err != nil {
		t.Fatalf("read late repository: %v", err)
	}
	if repo.PullCount != 1 {
		t.Fatalf("late repository pull count = %d, want 1", repo.PullCount)
	}
	firstRepo, err := store.RepositoryByName(t.Context(), upstream.ID, "library/first")
	if err != nil {
		t.Fatalf("read first repository after retry: %v", err)
	}
	if firstRepo.PullCount != 1 {
		t.Fatalf("first repository pull count after retry = %d, want 1", firstRepo.PullCount)
	}
	if upstream.PullCount != 2 {
		t.Fatalf("upstream pull count after retry = %d, want 2", upstream.PullCount)
	}
}
