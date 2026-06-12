package meta

import (
	"context"
	"database/sql"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/querydsl"
)

type repositoryPullAggregateRow struct {
	PullCount             sql.NullInt64 `dbx:"pull_count"`
	PolicyDeniedPullCount sql.NullInt64 `dbx:"policy_denied_pull_count"`
	LastPullAt            sql.NullInt64 `dbx:"last_pull_at"`
	LastUpstreamPullAt    sql.NullInt64 `dbx:"last_upstream_pull_at"`
	LastPolicyDeniedAt    sql.NullInt64 `dbx:"last_policy_denied_at"`
}

type repositoryBlobAggregateRow struct {
	BlobLinkCount  sql.NullInt64 `dbx:"blob_link_count"`
	BlobBytes      sql.NullInt64 `dbx:"blob_bytes"`
	LastAccessAt   sql.NullInt64 `dbx:"last_access_at"`
	LastVerifiedAt sql.NullInt64 `dbx:"last_verified_at"`
	LastRepoBlobAt sql.NullInt64 `dbx:"last_repo_blob_at"`
}

type upstreamAggregateRow struct {
	RepositoryCount       sql.NullInt64 `dbx:"repository_count"`
	PullCount             sql.NullInt64 `dbx:"pull_count"`
	PolicyDeniedPullCount sql.NullInt64 `dbx:"policy_denied_pull_count"`
	BlobBytes             sql.NullInt64 `dbx:"blob_bytes"`
	BlobLinkCount         sql.NullInt64 `dbx:"blob_link_count"`
	LastPolicyDeniedAt    sql.NullInt64 `dbx:"last_policy_denied_at"`
	LastActivityAt        sql.NullInt64 `dbx:"last_activity_at"`
}

type repositoryAggregate struct {
	PullCount             int64
	PolicyDeniedPullCount int64
	BlobBytes             int64
	BlobLinkCount         int64
	LastPullAt            time.Time
	LastPolicyDeniedAt    time.Time
	LastBlobAccessAt      time.Time
	LastActivityAt        time.Time
}

func (s *SQLStore) repositoryAggregate(ctx context.Context, alias, name string) (repositoryAggregate, error) {
	pulls, err := s.repositoryPullAggregate(ctx, alias, name)
	if err != nil {
		return repositoryAggregate{}, err
	}
	blobs, err := s.repositoryBlobAggregate(ctx, alias, name)
	if err != nil {
		return repositoryAggregate{}, err
	}
	manifestAt, err := s.repositoryMaxUpdatedAt(ctx, sqlManifestRows, sqlManifestRows.Alias, sqlManifestRows.Repository, sqlManifestRows.UpdatedAt, alias, name, "manifest")
	if err != nil {
		return repositoryAggregate{}, err
	}
	tagAt, err := s.repositoryMaxUpdatedAt(ctx, sqlTagRows, sqlTagRows.Alias, sqlTagRows.Repository, sqlTagRows.UpdatedAt, alias, name, "tag")
	if err != nil {
		return repositoryAggregate{}, err
	}
	lastPullAt := nullUnixNanoTime(pulls.LastPullAt)
	lastPolicyDeniedAt := nullUnixNanoTime(pulls.LastPolicyDeniedAt)
	lastBlobAccessAt := nullUnixNanoTime(blobs.LastAccessAt)
	return repositoryAggregate{
		PullCount:             nullInt64(pulls.PullCount),
		PolicyDeniedPullCount: nullInt64(pulls.PolicyDeniedPullCount),
		BlobBytes:             nullInt64(blobs.BlobBytes),
		BlobLinkCount:         nullInt64(blobs.BlobLinkCount),
		LastPullAt:            lastPullAt,
		LastPolicyDeniedAt:    lastPolicyDeniedAt,
		LastBlobAccessAt:      lastBlobAccessAt,
		LastActivityAt: maxTime(
			lastPullAt,
			nullUnixNanoTime(pulls.LastUpstreamPullAt),
			lastPolicyDeniedAt,
			lastBlobAccessAt,
			nullUnixNanoTime(blobs.LastVerifiedAt),
			nullUnixNanoTime(blobs.LastRepoBlobAt),
			manifestAt,
			tagAt,
		),
	}, nil
}

func (s *SQLStore) repositoryPullAggregate(ctx context.Context, alias, name string) (repositoryPullAggregateRow, error) {
	row, err := dbx.GetTyped[repositoryPullAggregateRow](ctx, s.db, querydsl.SelectInto[repositoryPullAggregateRow](
		querydsl.Sum(sqlPullRows.Count).As("pull_count"),
		querydsl.Sum(sqlPullRows.PolicyDeniedCount).As("policy_denied_pull_count"),
		querydsl.Max(sqlPullRows.LastPullAt).As("last_pull_at"),
		querydsl.Max(sqlPullRows.LastUpstreamPullAt).As("last_upstream_pull_at"),
		querydsl.Max(sqlPullRows.LastPolicyDeniedAt).As("last_policy_denied_at"),
	).From(sqlPullRows).Where(querydsl.And(
		sqlPullRows.Alias.Eq(alias),
		sqlPullRows.Repository.Eq(name),
	)))
	if err != nil {
		return repositoryPullAggregateRow{}, wrapError(err, "aggregate repository pull metadata")
	}
	return row, nil
}

func (s *SQLStore) repositoryBlobAggregate(ctx context.Context, alias, name string) (repositoryBlobAggregateRow, error) {
	row, err := dbx.GetTyped[repositoryBlobAggregateRow](ctx, s.db, querydsl.SelectInto[repositoryBlobAggregateRow](
		querydsl.Count(sqlRepoBlobRows.ID).As("blob_link_count"),
		querydsl.Sum(sqlBlobRows.Size).As("blob_bytes"),
		querydsl.Max(sqlRepoBlobRows.LastAccessAt).As("last_access_at"),
		querydsl.Max(sqlRepoBlobRows.LastVerifiedAt).As("last_verified_at"),
		querydsl.Max(sqlRepoBlobRows.UpdatedAt).As("last_repo_blob_at"),
	).From(sqlRepoBlobRows).
		LeftJoin(sqlBlobRows).On(sqlRepoBlobRows.Digest.EqColumn(sqlBlobRows.Digest)).
		Where(querydsl.And(
			sqlRepoBlobRows.Alias.Eq(alias),
			sqlRepoBlobRows.Repository.Eq(name),
		)))
	if err != nil {
		return repositoryBlobAggregateRow{}, wrapError(err, "aggregate repository blob metadata")
	}
	return row, nil
}
