package meta

import (
	"context"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/querydsl"
)

func (s *SQLiteStore) repositoryMaxUpdatedAt(
	ctx context.Context,
	source querydsl.TableSource,
	aliasColumn querydsl.TypedOperand[string],
	repositoryColumn querydsl.TypedOperand[string],
	updatedColumn querydsl.TypedOperand[int64],
	alias string,
	name string,
	label string,
) (time.Time, error) {
	row, err := dbx.GetTyped[sumInt64Row](ctx, s.db, querydsl.SelectInto[sumInt64Row](
		querydsl.Max(updatedColumn).As("value"),
	).From(source).Where(querydsl.And(
		querydsl.CompareValue(aliasColumn, querydsl.OpEq, alias),
		querydsl.CompareValue(repositoryColumn, querydsl.OpEq, name),
	)))
	if err != nil {
		return time.Time{}, wrapError(err, "get repository %s update time", label)
	}
	return nullUnixNanoTime(row.Value), nil
}

func (s *SQLiteStore) upstreamAggregate(ctx context.Context, upstreamID int64) (upstreamAggregateRow, error) {
	row, err := dbx.GetTyped[upstreamAggregateRow](ctx, s.db, querydsl.SelectInto[upstreamAggregateRow](
		querydsl.Count(sqliteRepositoryRows.ID).As("repository_count"),
		querydsl.Sum(sqliteRepositoryRows.PullCount).As("pull_count"),
		querydsl.Sum(sqliteRepositoryRows.BlobBytes).As("blob_bytes"),
		querydsl.Sum(sqliteRepositoryRows.BlobLinkCount).As("blob_link_count"),
		querydsl.Max(sqliteRepositoryRows.LastActivityAt).As("last_activity_at"),
	).From(sqliteRepositoryRows).Where(sqliteRepositoryRows.UpstreamID.Eq(upstreamID)))
	if err != nil {
		return upstreamAggregateRow{}, wrapError(err, "aggregate upstream metadata")
	}
	return row, nil
}
