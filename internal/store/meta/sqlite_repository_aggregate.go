package meta

import (
	"context"
	"database/sql"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/querydsl"
)

type repositoryPullAggregateRow struct {
	PullCount          sql.NullInt64 `dbx:"pull_count"`
	LastPullAt         sql.NullInt64 `dbx:"last_pull_at"`
	LastUpstreamPullAt sql.NullInt64 `dbx:"last_upstream_pull_at"`
}

type repositoryBlobAggregateRow struct {
	BlobLinkCount  sql.NullInt64 `dbx:"blob_link_count"`
	BlobBytes      sql.NullInt64 `dbx:"blob_bytes"`
	LastAccessAt   sql.NullInt64 `dbx:"last_access_at"`
	LastVerifiedAt sql.NullInt64 `dbx:"last_verified_at"`
	LastRepoBlobAt sql.NullInt64 `dbx:"last_repo_blob_at"`
}

type upstreamAggregateRow struct {
	RepositoryCount sql.NullInt64 `dbx:"repository_count"`
	PullCount       sql.NullInt64 `dbx:"pull_count"`
	BlobBytes       sql.NullInt64 `dbx:"blob_bytes"`
	BlobLinkCount   sql.NullInt64 `dbx:"blob_link_count"`
	LastActivityAt  sql.NullInt64 `dbx:"last_activity_at"`
}

type repositoryAggregate struct {
	PullCount        int64
	BlobBytes        int64
	BlobLinkCount    int64
	LastPullAt       time.Time
	LastBlobAccessAt time.Time
	LastActivityAt   time.Time
}

func (s *SQLiteStore) repositoryAggregate(ctx context.Context, alias, name string) (repositoryAggregate, error) {
	pulls, err := s.repositoryPullAggregate(ctx, alias, name)
	if err != nil {
		return repositoryAggregate{}, err
	}
	blobs, err := s.repositoryBlobAggregate(ctx, alias, name)
	if err != nil {
		return repositoryAggregate{}, err
	}
	manifestAt, err := s.repositoryMaxUpdatedAt(ctx, sqliteManifestRows, sqliteManifestRows.Alias, sqliteManifestRows.Repository, sqliteManifestRows.UpdatedAt, alias, name, "manifest")
	if err != nil {
		return repositoryAggregate{}, err
	}
	tagAt, err := s.repositoryMaxUpdatedAt(ctx, sqliteTagRows, sqliteTagRows.Alias, sqliteTagRows.Repository, sqliteTagRows.UpdatedAt, alias, name, "tag")
	if err != nil {
		return repositoryAggregate{}, err
	}
	lastPullAt := nullUnixNanoTime(pulls.LastPullAt)
	lastBlobAccessAt := nullUnixNanoTime(blobs.LastAccessAt)
	return repositoryAggregate{
		PullCount:        nullInt64(pulls.PullCount),
		BlobBytes:        nullInt64(blobs.BlobBytes),
		BlobLinkCount:    nullInt64(blobs.BlobLinkCount),
		LastPullAt:       lastPullAt,
		LastBlobAccessAt: lastBlobAccessAt,
		LastActivityAt: maxTime(
			lastPullAt,
			nullUnixNanoTime(pulls.LastUpstreamPullAt),
			lastBlobAccessAt,
			nullUnixNanoTime(blobs.LastVerifiedAt),
			nullUnixNanoTime(blobs.LastRepoBlobAt),
			manifestAt,
			tagAt,
		),
	}, nil
}

func (s *SQLiteStore) repositoryPullAggregate(ctx context.Context, alias, name string) (repositoryPullAggregateRow, error) {
	row, err := dbx.GetTyped[repositoryPullAggregateRow](ctx, s.db, querydsl.SelectInto[repositoryPullAggregateRow](
		querydsl.Sum(sqlitePullRows.Count).As("pull_count"),
		querydsl.Max(sqlitePullRows.LastPullAt).As("last_pull_at"),
		querydsl.Max(sqlitePullRows.LastUpstreamPullAt).As("last_upstream_pull_at"),
	).From(sqlitePullRows).Where(querydsl.And(
		sqlitePullRows.Alias.Eq(alias),
		sqlitePullRows.Repository.Eq(name),
	)))
	if err != nil {
		return repositoryPullAggregateRow{}, wrapError(err, "aggregate repository pull metadata")
	}
	return row, nil
}

func (s *SQLiteStore) repositoryBlobAggregate(ctx context.Context, alias, name string) (repositoryBlobAggregateRow, error) {
	row, err := dbx.GetTyped[repositoryBlobAggregateRow](ctx, s.db, querydsl.SelectInto[repositoryBlobAggregateRow](
		querydsl.Count(sqliteRepoBlobRows.ID).As("blob_link_count"),
		querydsl.Sum(sqliteBlobRows.Size).As("blob_bytes"),
		querydsl.Max(sqliteRepoBlobRows.LastAccessAt).As("last_access_at"),
		querydsl.Max(sqliteRepoBlobRows.LastVerifiedAt).As("last_verified_at"),
		querydsl.Max(sqliteRepoBlobRows.UpdatedAt).As("last_repo_blob_at"),
	).From(sqliteRepoBlobRows).
		LeftJoin(sqliteBlobRows).On(sqliteRepoBlobRows.Digest.EqColumn(sqliteBlobRows.Digest)).
		Where(querydsl.And(
			sqliteRepoBlobRows.Alias.Eq(alias),
			sqliteRepoBlobRows.Repository.Eq(name),
		)))
	if err != nil {
		return repositoryBlobAggregateRow{}, wrapError(err, "aggregate repository blob metadata")
	}
	return row, nil
}
