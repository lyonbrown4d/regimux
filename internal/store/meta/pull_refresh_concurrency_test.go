package meta_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/stretchr/testify/require"
)

func TestSQLStoreRecordPullConcurrentCatalogConsistency(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	key := meta.PullKey{Alias: "hub", Repository: "library/node", Reference: "20"}
	const workers = 512
	runConcurrentPullWrites(ctx, t, store, key, base, workers)
	lastPullAt := base.Add((workers - 1) * time.Second)
	upstreamAt, deniedAt := recordUpstreamDeniedAndOlder(ctx, t, store, key, lastPullAt)
	assertPullCatalog(ctx, t, store, key, workers+1, lastPullAt, upstreamAt, deniedAt)
}

func runConcurrentPullWrites(ctx context.Context, t *testing.T, store *meta.SQLStore, key meta.PullKey, base time.Time, count int) {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, count)
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(count)
	done.Add(count)
	for index := range count {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			if _, err := store.RecordPull(ctx, key, base.Add(time.Duration(index)*time.Second)); err != nil {
				errs <- fmt.Errorf("record concurrent pull: %w", err)
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	close(errs)
	for err := range errs {
		requireNoError(t, "record concurrent pull", err)
	}
}

func recordUpstreamDeniedAndOlder(ctx context.Context, t *testing.T, store *meta.SQLStore, key meta.PullKey, lastPullAt time.Time) (time.Time, time.Time) {
	t.Helper()
	upstreamAt := lastPullAt.Add(time.Hour)
	deniedAt := upstreamAt.Add(time.Hour)
	if _, err := store.RecordUpstreamPull(ctx, key, upstreamAt); err != nil {
		t.Fatalf("record upstream pull: %v", err)
	}
	if _, err := store.RecordPolicyDeniedPull(ctx, key, deniedAt); err != nil {
		t.Fatalf("record policy denied pull: %v", err)
	}
	if _, err := store.RecordPull(ctx, key, lastPullAt.Add(-time.Hour)); err != nil {
		t.Fatalf("record out-of-order old pull: %v", err)
	}
	return upstreamAt, deniedAt
}

func assertPullCatalog(
	ctx context.Context,
	t *testing.T,
	store *meta.SQLStore,
	key meta.PullKey,
	wantPulls int,
	lastPullAt, upstreamAt, deniedAt time.Time,
) {
	t.Helper()
	pull, ok, err := store.Pull(ctx, key)
	requireNoError(t, "get final pull record", err)
	require.True(t, ok)
	require.NotNil(t, pull)
	require.Equal(t, int64(wantPulls), pull.Count)
	require.Equal(t, int64(1), pull.PolicyDeniedCount)
	require.Equal(t, lastPullAt, pull.LastPullAt)
	require.Equal(t, upstreamAt, pull.LastUpstreamPullAt)
	require.Equal(t, deniedAt, pull.LastPolicyDeniedAt)

	upstream, err := store.UpstreamByAlias(ctx, key.Alias)
	requireNoError(t, "get refreshed upstream", err)
	require.NotNil(t, upstream)
	repository, err := store.RepositoryByName(ctx, upstream.ID, key.Repository)
	requireNoError(t, "get refreshed repository", err)
	require.NotNil(t, repository)
	require.Equal(t, int64(wantPulls), repository.PullCount)
	require.Equal(t, int64(1), repository.PolicyDeniedPullCount)
	require.Equal(t, deniedAt, repository.LastActivityAt)
	require.Equal(t, deniedAt, repository.UpdatedAt)
	require.Equal(t, deniedAt, upstream.UpdatedAt)
	require.Equal(t, int64(wantPulls), upstream.PullCount)
	require.Equal(t, int64(1), upstream.PolicyDeniedPullCount)
	require.Equal(t, deniedAt, upstream.LastActivityAt)
}
