package meta

import (
	"context"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/querydsl"
)

func (s *SQLStore) repositoryMaxUpdatedAt(
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

func (s *SQLStore) upstreamAggregate(ctx context.Context, upstreamID int64) (upstreamAggregateRow, error) {
	row, err := dbx.GetTyped[upstreamAggregateRow](ctx, s.db, querydsl.SelectInto[upstreamAggregateRow](
		querydsl.Count(sqlRepositoryRows.ID).As("repository_count"),
		querydsl.Sum(sqlRepositoryRows.PullCount).As("pull_count"),
		querydsl.Sum(sqlRepositoryRows.BlobBytes).As("blob_bytes"),
		querydsl.Sum(sqlRepositoryRows.BlobLinkCount).As("blob_link_count"),
		querydsl.Max(sqlRepositoryRows.LastActivityAt).As("last_activity_at"),
	).From(sqlRepositoryRows).Where(sqlRepositoryRows.UpstreamID.Eq(upstreamID)))
	if err != nil {
		return upstreamAggregateRow{}, wrapError(err, "aggregate upstream metadata")
	}
	return row, nil
}
