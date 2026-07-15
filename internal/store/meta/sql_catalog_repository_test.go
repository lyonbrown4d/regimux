package meta_test

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestSQLStoreRepositoryMetadataAggregates(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)
	first := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	repoKey := meta.PullKey{Alias: "hub", Repository: "library/node", Reference: "20"}
	deniedAt := seedRepositoryAggregateRecords(ctx, t, store, repoKey, first, second)

	upstream, err := store.UpstreamByAlias(ctx, "hub")
	requireNoError(t, "get upstream metadata", err)
	repository, err := store.RepositoryByName(ctx, upstream.ID, "library/node")
	requireNoError(t, "get repository metadata", err)
	assertRepositoryAggregate(t, repository, 2, 1, 400, 2, second, deniedAt, second.Add(time.Minute))

	repositories, err := store.ListRepositories(ctx, meta.RepositoryListRecentFirst())
	requireNoError(t, "list repository metadata", err)
	if repositories.Len() != 1 || repositories.Values()[0].Name != "library/node" {
		t.Fatalf("unexpected repositories: %#v", repositories)
	}
	upstreams, err := store.ListUpstreams(ctx, meta.UpstreamListRecentFirst())
	requireNoError(t, "list upstream metadata", err)
	if upstreams.Len() != 1 || upstreams.Values()[0].RepositoryCount != 1 || upstreams.Values()[0].PullCount != 2 || upstreams.Values()[0].PolicyDeniedPullCount != 1 || upstreams.Values()[0].BlobBytes != 400 {
		t.Fatalf("unexpected upstream aggregates: %#v", upstreams)
	}

	stats, err := store.MetadataStats(ctx, second)
	requireNoError(t, "metadata stats", err)
	if stats.RepositoryCount != 1 || stats.RepositoryBytes != 400 {
		t.Fatalf("unexpected repository stats: %#v", stats)
	}

	err = store.DeleteRepoBlob(ctx, meta.RepoBlobKey{
		Alias:      "hub",
		Repository: "library/node",
		Digest:     secondTestDigest,
	})
	requireNoError(t, "delete repo blob", err)
	repository, err = store.RepositoryByName(ctx, upstream.ID, "library/node")
	requireNoError(t, "get repository metadata after delete", err)
	assertRepositoryAggregate(t, repository, 2, 1, 100, 1, second, deniedAt, second)
}

func seedRepositoryAggregateRecords(
	ctx context.Context,
	t *testing.T,
	store *meta.SQLStore,
	repoKey meta.PullKey,
	first time.Time,
	second time.Time,
) time.Time {
	t.Helper()
	_, err := store.RecordPull(ctx, repoKey, first)
	requireNoError(t, "record pull", err)
	_, err = store.RecordPull(ctx, repoKey, second)
	requireNoError(t, "record second pull", err)
	deniedAt := second.Add(30 * time.Minute)
	_, err = store.RecordPolicyDeniedPull(ctx, repoKey, deniedAt)
	requireNoError(t, "record policy denied pull", err)
	_, err = store.UpsertBlob(ctx, meta.BlobRecord{Digest: testDigest, Size: 100})
	requireNoError(t, "upsert blob", err)
	_, err = store.UpsertBlob(ctx, meta.BlobRecord{Digest: secondTestDigest, Size: 300})
	requireNoError(t, "upsert second blob", err)
	_, err = store.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:        "hub",
		Repository:   "library/node",
		Digest:       testDigest,
		LastAccessAt: second,
	})
	requireNoError(t, "upsert repo blob", err)
	_, err = store.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:        "hub",
		Repository:   "library/node",
		Digest:       secondTestDigest,
		LastAccessAt: second.Add(time.Minute),
	})
	requireNoError(t, "upsert second repo blob", err)
	return deniedAt
}
