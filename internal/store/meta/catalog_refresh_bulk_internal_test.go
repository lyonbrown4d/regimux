package meta

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
)

func TestRepositoryMetadataRefreshBulkEnsuresRepositoriesInChunks(t *testing.T) {
	const (
		alias           = "bulk-refresh"
		repositoryCount = 512
		existingName    = "library/repo-000"
	)
	hook := &bulkRepositoryInsertHook{}
	store := openBulkRepositoryStore(t, hook)
	base := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	upstreamID, before := seedBulkRepositoryCatalog(t, store, alias, existingName, base)
	hook.armed.Store(true)
	if err := store.refreshRepositoryMetadataBatch(t.Context(), alias, bulkRepositoryUpdates(base, repositoryCount)); err != nil {
		t.Fatalf("refresh %d repository rows: %v", repositoryCount, err)
	}
	assertBulkRepositoryInsertBatch(t, hook, repositoryCount)
	assertBulkRepositoryCatalog(t, store, upstreamID, alias, existingName, before, repositoryCount)
}

func openBulkRepositoryStore(t *testing.T, hook *bulkRepositoryInsertHook) *SQLStore {
	t.Helper()
	store, err := OpenSQLiteWithOptions(t.Context(), DBOptions{
		Path:  filepath.Join(t.TempDir(), "bulk-refresh.db"),
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

func seedBulkRepositoryCatalog(t *testing.T, store *SQLStore, alias, name string, at time.Time) (int64, *Repository) {
	t.Helper()
	seedBulkRepositoryPulls(t, store, alias, name, at)
	seedBulkRepositoryBlobs(t, store, alias, name)
	return readBulkRepositoryIdentity(t, store, alias, name)
}

func seedBulkRepositoryPulls(t *testing.T, store *SQLStore, alias, name string, at time.Time) {
	t.Helper()
	key := PullKey{Alias: alias, Repository: name, Reference: "stable"}
	for range 2 {
		if _, err := store.RecordPull(t.Context(), key, at); err != nil {
			t.Fatalf("seed repository pull: %v", err)
		}
	}
	if _, err := store.RecordPolicyDeniedPull(t.Context(), key, at.Add(time.Minute)); err != nil {
		t.Fatalf("seed denied repository pull: %v", err)
	}
}

func seedBulkRepositoryBlobs(t *testing.T, store *SQLStore, alias, name string) {
	t.Helper()
	firstDigest := "sha256:" + strings.Repeat("a", 64)
	secondDigest := "sha256:" + strings.Repeat("b", 64)
	for digest, size := range map[string]int64{firstDigest: 100, secondDigest: 300} {
		if _, err := store.UpsertBlob(t.Context(), BlobRecord{Digest: digest, Size: size}); err != nil {
			t.Fatalf("seed blob %s: %v", digest, err)
		}
		if _, err := store.UpsertRepoBlob(t.Context(), RepoBlobRecord{Alias: alias, Repository: name, Digest: digest}); err != nil {
			t.Fatalf("seed repository blob %s: %v", digest, err)
		}
	}
}

func readBulkRepositoryIdentity(t *testing.T, store *SQLStore, alias, name string) (int64, *Repository) {
	t.Helper()
	upstream, err := store.UpstreamByAlias(t.Context(), alias)
	if err != nil {
		t.Fatalf("read seeded upstream: %v", err)
	}
	repository, err := store.RepositoryByName(t.Context(), upstream.ID, name)
	if err != nil {
		t.Fatalf("read seeded repository: %v", err)
	}
	return upstream.ID, repository
}

func bulkRepositoryUpdates(at time.Time, count int) map[string]time.Time {
	updates := make(map[string]time.Time, count)
	for index := range count {
		updates[fmtBulkRepositoryName(index)] = at.Add(time.Hour)
	}
	return updates
}

func assertBulkRepositoryInsertBatch(t *testing.T, hook *bulkRepositoryInsertHook, count int) {
	t.Helper()
	insertRows := hook.repositoryInsertRows()
	if len(insertRows) != 16 {
		t.Fatalf("repository INSERT statements = %d, want 16 chunks", len(insertRows))
	}
	inserted := 0
	for _, rows := range insertRows {
		if rows < 2 {
			t.Fatalf("repository INSERT had %d VALUES rows, want a multi-row insert", rows)
		}
		inserted += rows
	}
	if inserted != count {
		t.Fatalf("repository INSERT VALUES rows = %d, want %d", inserted, count)
	}
	if got := hook.upstreamAggregates.Load(); got != 1 {
		t.Fatalf("upstream aggregate queries = %d, want 1", got)
	}
}

func assertBulkRepositoryCatalog(t *testing.T, store *SQLStore, upstreamID int64, alias, name string, before *Repository, count int) {
	t.Helper()
	assertBulkRepositoryRowCount(t, store, count)
	assertBulkRepositoryIdentity(t, store, upstreamID, name, before)
	assertBulkRepositoryStats(t, store, upstreamID, name)
	assertBulkUpstreamStats(t, store, alias, count)
}

func assertBulkRepositoryRowCount(t *testing.T, store *SQLStore, count int) {
	t.Helper()
	repositories, err := store.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("list refreshed repositories: %v", err)
	}
	if repositories.Len() != count {
		t.Fatalf("repository rows = %d, want %d", repositories.Len(), count)
	}
}

func assertBulkRepositoryIdentity(t *testing.T, store *SQLStore, upstreamID int64, name string, before *Repository) {
	t.Helper()
	after, err := store.RepositoryByName(t.Context(), upstreamID, name)
	if err != nil {
		t.Fatalf("read refreshed seeded repository: %v", err)
	}
	if after.ID != before.ID || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("existing repository identity changed: before=%#v after=%#v", before, after)
	}
}

func assertBulkRepositoryStats(t *testing.T, store *SQLStore, upstreamID int64, name string) {
	t.Helper()
	after, err := store.RepositoryByName(t.Context(), upstreamID, name)
	if err != nil {
		t.Fatalf("read refreshed seeded repository aggregates: %v", err)
	}
	if after.PullCount != 2 {
		t.Fatalf("existing repository pull count = %d, want 2", after.PullCount)
	}
	if after.PolicyDeniedPullCount != 1 {
		t.Fatalf("existing repository denied pull count = %d, want 1", after.PolicyDeniedPullCount)
	}
	if after.BlobBytes != 400 {
		t.Fatalf("existing repository blob bytes = %d, want 400", after.BlobBytes)
	}
	if after.BlobLinkCount != 2 {
		t.Fatalf("existing repository blob links = %d, want 2", after.BlobLinkCount)
	}
}

func assertBulkUpstreamStats(t *testing.T, store *SQLStore, alias string, count int) {
	t.Helper()
	upstream, err := store.UpstreamByAlias(t.Context(), alias)
	if err != nil {
		t.Fatalf("read refreshed upstream: %v", err)
	}
	if upstream.RepositoryCount != int64(count) {
		t.Fatalf("upstream repository count = %d, want %d", upstream.RepositoryCount, count)
	}
	if upstream.PullCount != 2 {
		t.Fatalf("upstream pull count = %d, want 2", upstream.PullCount)
	}
	if upstream.PolicyDeniedPullCount != 1 {
		t.Fatalf("upstream denied pull count = %d, want 1", upstream.PolicyDeniedPullCount)
	}
	if upstream.BlobBytes != 400 {
		t.Fatalf("upstream blob bytes = %d, want 400", upstream.BlobBytes)
	}
	if upstream.BlobLinkCount != 2 {
		t.Fatalf("upstream blob links = %d, want 2", upstream.BlobLinkCount)
	}
}

func fmtBulkRepositoryName(index int) string {
	return fmt.Sprintf("library/repo-%03d", index)
}

type bulkRepositoryInsertHook struct {
	armed              atomic.Bool
	upstreamAggregates atomic.Int64
	mu                 sync.Mutex
	insertRows         []int
}

func (h *bulkRepositoryInsertHook) dbHook() dbx.Hook {
	return dbx.HookFuncs{AfterFunc: h.after}
}

func (h *bulkRepositoryInsertHook) after(_ context.Context, event *dbx.HookEvent) {
	if !h.armed.Load() || event.Err != nil {
		return
	}
	if (event.Operation == dbx.OperationQuery || event.Operation == dbx.OperationQueryRow) && isUpstreamAggregateQuery(event.SQL) {
		h.upstreamAggregates.Add(1)
	}
	if event.Operation != dbx.OperationExec || !isRepositoryMetadataInsert(event.SQL) {
		return
	}
	h.mu.Lock()
	h.insertRows = append(h.insertRows, countInsertValueRows(event.SQL))
	h.mu.Unlock()
}

func (h *bulkRepositoryInsertHook) repositoryInsertRows() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int(nil), h.insertRows...)
}

func isRepositoryMetadataInsert(statement string) bool {
	statement = strings.ToLower(strings.TrimSpace(statement))
	return strings.HasPrefix(statement, "insert") && strings.Contains(statement, "meta_repositories")
}

var insertValueSeparator = regexp.MustCompile(`\)\s*,\s*\(`)

func countInsertValueRows(statement string) int {
	return 1 + len(insertValueSeparator.FindAllStringIndex(statement, -1))
}
