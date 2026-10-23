package meta

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogRefreshWaitHonorsCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "canceled", true: "deadline"}[deadline], func(t *testing.T) {
			testCatalogRefreshCancellation(t, deadline)
		})
	}
}

func testCatalogRefreshCancellation(t *testing.T, deadline bool) {
	t.Helper()
	store, err := OpenSQLiteWithOptions(t.Context(), DBOptions{Path: filepath.Join(t.TempDir(), "meta.db")})
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close metadata: %v", closeErr)
		}
	})
	if err := store.catalogMu.Acquire(t.Context(), 1); err != nil {
		t.Fatalf("hold catalog gate: %v", err)
	}
	t.Cleanup(func() { store.catalogMu.Release(1) })
	ctx, cancel, expected := catalogWaitContext(t, deadline)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- store.refreshRepositoryMetadata(ctx, "hub", "library/demo", time.Now()) }()
	select {
	case err := <-result:
		if !errors.Is(err, expected) {
			t.Fatalf("catalog wait error = %v, want %v", err, expected)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled refresh waited for the catalog gate to be released")
	}
	waitForCatalogRefreshStatesReleased(t, store)
}

func waitForCatalogRefreshStatesReleased(t *testing.T, store *SQLStore) {
	t.Helper()
	cleanupDeadline := time.Now().Add(time.Second)
	for {
		active := activeCatalogRefreshStates(store)
		if active == 0 {
			return
		}
		if time.Now().After(cleanupDeadline) {
			t.Fatalf("canceled refresh retained %d inactive repository states", active)
		}
		time.Sleep(time.Millisecond)
	}
}

func activeCatalogRefreshStates(store *SQLStore) int {
	store.catalogRefreshMu.Lock()
	defer store.catalogRefreshMu.Unlock()
	return len(store.catalogRefreshes)
}

func catalogWaitContext(t *testing.T, deadline bool) (context.Context, context.CancelFunc, error) {
	t.Helper()
	if deadline {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		return ctx, cancel, context.DeadlineExceeded
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx, cancel, context.Canceled
}
