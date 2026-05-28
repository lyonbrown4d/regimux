package meta

import (
	"context"
	"database/sql"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/querydsl"
	"github.com/arcgolabs/dbx/repository"
)

type sumInt64Row struct {
	Value sql.NullInt64 `dbx:"value"`
}

type pullTimesStatsRow struct {
	LastPullAt         sql.NullInt64 `dbx:"last_pull_at"`
	LastUpstreamPullAt sql.NullInt64 `dbx:"last_upstream_pull_at"`
}

func (s *SQLiteStore) MetadataStats(ctx context.Context, now time.Time) (MetadataStats, error) {
	stats := MetadataStats{}
	for _, load := range []func(context.Context, time.Time, *MetadataStats) error{
		s.loadManifestStats,
		s.loadTagStats,
		s.loadBlobStats,
		s.loadRepositoryBlobStats,
		s.loadPullStats,
	} {
		if err := load(ctx, now, &stats); err != nil {
			return MetadataStats{}, err
		}
	}
	return stats, nil
}

func (s *SQLiteStore) loadManifestStats(ctx context.Context, now time.Time, stats *MetadataStats) error {
	count, err := s.manifest.CountSpec(ctx)
	if err != nil {
		return wrapError(err, "count manifest metadata")
	}
	expired, err := s.expiredManifestCount(ctx, now)
	if err != nil {
		return err
	}
	bytes, err := s.sumInt64(ctx, sqliteManifestRows, sqliteManifestRows.Size, "manifest metadata bytes")
	if err != nil {
		return err
	}
	stats.ManifestCount = count
	stats.ExpiredManifestCount = expired
	stats.ManifestBytes = bytes
	return nil
}

func (s *SQLiteStore) loadTagStats(ctx context.Context, now time.Time, stats *MetadataStats) error {
	count, err := s.tags.CountSpec(ctx)
	if err != nil {
		return wrapError(err, "count tag metadata")
	}
	expired, err := s.expiredTagCount(ctx, now)
	if err != nil {
		return err
	}
	stats.TagCount = count
	stats.ExpiredTagCount = expired
	return nil
}

func (s *SQLiteStore) loadBlobStats(ctx context.Context, _ time.Time, stats *MetadataStats) error {
	count, err := s.blobs.CountSpec(ctx)
	if err != nil {
		return wrapError(err, "count blob metadata")
	}
	bytes, err := s.sumInt64(ctx, sqliteBlobRows, sqliteBlobRows.Size, "blob metadata bytes")
	if err != nil {
		return err
	}
	stats.BlobCount = count
	stats.BlobBytes = bytes
	return nil
}

func (s *SQLiteStore) loadRepositoryBlobStats(ctx context.Context, _ time.Time, stats *MetadataStats) error {
	count, err := s.repoBlobs.CountSpec(ctx)
	if err != nil {
		return wrapError(err, "count repository blob metadata")
	}
	stats.RepoBlobCount = count
	return nil
}

func (s *SQLiteStore) loadPullStats(ctx context.Context, _ time.Time, stats *MetadataStats) error {
	count, err := s.pulls.CountSpec(ctx)
	if err != nil {
		return wrapError(err, "count pull metadata")
	}
	lastPullAt, lastUpstreamPullAt, err := s.latestPullTimes(ctx)
	if err != nil {
		return err
	}
	stats.PullCount = count
	stats.LastPullAt = lastPullAt
	stats.LastUpstreamPullAt = lastUpstreamPullAt
	return nil
}

func (s *SQLiteStore) expiredManifestCount(ctx context.Context, now time.Time) (int64, error) {
	expiresAt := unixNano(now)
	count, err := s.manifest.CountSpec(ctx, repository.Where(querydsl.And(
		sqliteManifestRows.ExpiresAt.Ne(0),
		sqliteManifestRows.ExpiresAt.Le(expiresAt),
	)))
	if err != nil {
		return 0, wrapError(err, "count expired manifest metadata")
	}
	return count, nil
}

func (s *SQLiteStore) expiredTagCount(ctx context.Context, now time.Time) (int64, error) {
	expiresAt := unixNano(now)
	count, err := s.tags.CountSpec(ctx, repository.Where(querydsl.And(
		sqliteTagRows.ExpiresAt.Ne(0),
		sqliteTagRows.ExpiresAt.Le(expiresAt),
	)))
	if err != nil {
		return 0, wrapError(err, "count expired tag metadata")
	}
	return count, nil
}

func (s *SQLiteStore) sumInt64(
	ctx context.Context,
	source querydsl.TableSource,
	column querydsl.TypedOperand[int64],
	label string,
) (int64, error) {
	row, err := dbx.GetTyped[sumInt64Row](ctx, s.db, querydsl.SelectInto[sumInt64Row](
		querydsl.Sum(column).As("value"),
	).From(source))
	if err != nil {
		return 0, wrapError(err, "sum %s", label)
	}
	if !row.Value.Valid {
		return 0, nil
	}
	return row.Value.Int64, nil
}

func (s *SQLiteStore) latestPullTimes(ctx context.Context) (time.Time, time.Time, error) {
	row, err := dbx.GetTyped[pullTimesStatsRow](ctx, s.db, querydsl.SelectInto[pullTimesStatsRow](
		querydsl.Max(sqlitePullRows.LastPullAt).As("last_pull_at"),
		querydsl.Max(sqlitePullRows.LastUpstreamPullAt).As("last_upstream_pull_at"),
	).From(sqlitePullRows))
	if err != nil {
		return time.Time{}, time.Time{}, wrapError(err, "get latest pull metadata times")
	}
	return nullUnixNanoTime(row.LastPullAt), nullUnixNanoTime(row.LastUpstreamPullAt), nil
}

func nullUnixNanoTime(value sql.NullInt64) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return timeFromUnixNano(value.Int64)
}
