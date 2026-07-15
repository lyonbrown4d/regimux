package meta_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestSQLStoreRecordPullConcurrent(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)
	base := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	key := meta.PullKey{Alias: "hub", Repository: "library/node", Reference: "20"}
	const workers = 64

	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			<-start
			_, err := store.RecordPull(ctx, key, base.Add(time.Duration(i)*time.Nanosecond))
			if err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		requireNoError(t, "record concurrent pull", err)
	}

	got, ok, err := store.Pull(ctx, key)
	requireNoError(t, "get concurrent pull", err)
	if !ok {
		t.Fatal("expected concurrent pull record")
	}
	expectedLastPullAt := base.Add(time.Duration(workers-1) * time.Nanosecond)
	if got.Count != workers || !got.LastPullAt.Equal(expectedLastPullAt) {
		t.Fatalf("unexpected concurrent pull record: %#v", got)
	}
	repositories, err := store.ListRepositories(ctx)
	requireNoError(t, "list repositories after concurrent pull", err)
	if repositories.Len() != 1 || repositories.Values()[0].PullCount != workers {
		t.Fatalf("unexpected repository aggregate after concurrent pull: %#v", repositories)
	}
}
