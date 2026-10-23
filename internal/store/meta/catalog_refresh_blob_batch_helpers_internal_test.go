package meta

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
)

func openBlobCatalogRefreshStore(t *testing.T, hook *blobCatalogRefreshHook) *SQLStore {
	t.Helper()
	store, err := OpenSQLiteWithOptions(t.Context(), DBOptions{
		Path:  filepath.Join(t.TempDir(), "catalog-refresh.db"),
		Hooks: []dbx.Hook{hook.dbHook()},
	})
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() {
		hook.releaseAll()
		if err := store.Close(); err != nil {
			t.Errorf("close metadata store: %v", err)
		}
	})
	return store
}

func seedBlobCatalogRefresh(t *testing.T, store *SQLStore) string {
	t.Helper()
	digest := "sha256:" + strings.Repeat("a", 64)
	if _, err := store.UpsertBlob(t.Context(), BlobRecord{Digest: digest, Size: 100}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	for _, name := range []string{"library/first", "library/second"} {
		if _, err := store.UpsertRepoBlob(t.Context(), RepoBlobRecord{
			Alias: "hub", Repository: name, Digest: digest,
		}); err != nil {
			t.Fatalf("seed repository blob %s: %v", name, err)
		}
	}
	return digest
}

func assertBlobCatalogSize(t *testing.T, store *SQLStore, digest string, want int64) {
	t.Helper()
	upstream, err := store.UpstreamByAlias(t.Context(), "hub")
	if err != nil {
		t.Fatalf("read refreshed upstream: %v", err)
	}
	for _, name := range []string{"library/first", "library/second"} {
		repository, err := store.RepositoryByName(t.Context(), upstream.ID, name)
		if err != nil {
			t.Fatalf("read repository %s: %v", name, err)
		}
		if repository.BlobBytes != want {
			t.Errorf("repository %s blob bytes = %d, want %d (digest %s)", name, repository.BlobBytes, want, digest)
		}
	}
}

func assertNoCatalogRefreshStates(t *testing.T, store *SQLStore) {
	t.Helper()
	store.catalogRefreshMu.Lock()
	states := make(map[catalogRefreshKey]int, len(store.catalogRefreshes))
	for key, state := range store.catalogRefreshes {
		states[key] = state.users
	}
	store.catalogRefreshMu.Unlock()
	if len(states) != 0 {
		t.Fatalf("completed digest refresh retained catalog refresh states: %s", fmt.Sprint(states))
	}
}

func waitForCatalogRefreshKeyUsers(t *testing.T, store *SQLStore, key catalogRefreshKey, users int) bool {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		store.catalogRefreshMu.Lock()
		state := store.catalogRefreshes[key]
		registered := state != nil && state.users >= users
		store.catalogRefreshMu.Unlock()
		if registered {
			return true
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			return false
		}
	}
}

func waitForNoCatalogRefreshStates(t *testing.T, store *SQLStore) bool {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		store.catalogRefreshMu.Lock()
		active := len(store.catalogRefreshes)
		store.catalogRefreshMu.Unlock()
		if active == 0 {
			return true
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			return false
		}
	}
}

func waitForCatalogBatchRegistration(t *testing.T, store *SQLStore, key catalogRefreshKey, duplicateList <-chan struct{}) error {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		store.catalogRefreshMu.Lock()
		state := store.catalogRefreshes[key]
		registered := state != nil && state.users >= 2
		store.catalogRefreshMu.Unlock()
		if registered {
			return nil
		}
		select {
		case <-duplicateList:
			return errDuplicateDigestRefresh
		case <-ticker.C:
		case <-deadline.C:
			return errMissingDigestRegistration
		}
	}
}
