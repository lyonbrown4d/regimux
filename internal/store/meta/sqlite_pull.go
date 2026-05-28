package meta

import (
	"context"
	"errors"
	"time"

	"github.com/arcgolabs/dbx/paging"
	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLiteStore) Pull(ctx context.Context, key PullKey) (*PullRecord, bool, error) {
	key, err := normalizePullKey(key)
	if err != nil {
		return nil, false, err
	}
	row, err := repository.By(s.pulls, sqlitePullRows.Key).Get(ctx, key.String())
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get pull metadata")
	}
	return pullRowToRecord(row), true, nil
}

func (s *SQLiteStore) RecordPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error) {
	return s.recordPull(ctx, key, at, false)
}

func (s *SQLiteStore) RecordUpstreamPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error) {
	return s.recordPull(ctx, key, at, true)
}

func (s *SQLiteStore) recordPull(ctx context.Context, key PullKey, at time.Time, upstream bool) (*PullRecord, error) {
	key, err := normalizePullKey(key)
	if err != nil {
		return nil, err
	}
	now := at.UTC()
	if now.IsZero() {
		now = sqliteNow()
	}
	record := PullRecord{
		Key:        key.String(),
		Alias:      key.Alias,
		Repository: key.Repository,
		Reference:  key.Reference,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	existing, ok, err := s.Pull(ctx, key)
	if err != nil {
		return nil, err
	}
	if ok {
		record = *existing
		record.UpdatedAt = now
	}
	if upstream {
		record.LastUpstreamPullAt = now
	} else {
		record.Count++
		record.LastPullAt = now
	}
	row := pullRecordToRow(record)
	if record.ID != 0 {
		if err := s.updatePullRow(ctx, row); err != nil {
			return nil, err
		}
		return &record, nil
	}
	if err := s.pulls.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "record pull metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLiteStore) ListPulls(ctx context.Context, opts ...PullListOption) ([]PullRecord, error) {
	options := pullListOptions(opts...)
	query := repository.Query(s.pulls)
	if options.RecentFirst {
		query = query.OrderBy(
			sqlitePullRows.LastPullAt.Desc(),
			sqlitePullRows.UpdatedAt.Desc(),
			sqlitePullRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		page, err := query.ListPage(ctx, paging.NewRequest(1, options.Limit))
		if err != nil {
			return nil, wrapError(err, "list pull metadata")
		}
		return pullRowsToRecords(page.Items), nil
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list pull metadata")
	}
	return pullRowsToRecords(rows), nil
}

func pullRowsToRecords(rows interface {
	Len() int
	Range(func(int, pullRow) bool)
}) []PullRecord {
	records := make([]PullRecord, 0, rows.Len())
	rows.Range(func(_ int, row pullRow) bool {
		records = append(records, *pullRowToRecord(row))
		return true
	})
	return records
}

func (s *SQLiteStore) updatePullRow(ctx context.Context, row pullRow) error {
	_, err := repository.By(s.pulls, sqlitePullRows.Key).Update(ctx, row.Key,
		sqlitePullRows.Alias.Set(row.Alias),
		sqlitePullRows.Repository.Set(row.Repository),
		sqlitePullRows.Reference.Set(row.Reference),
		sqlitePullRows.Count.Set(row.Count),
		sqlitePullRows.LastPullAt.Set(row.LastPullAt),
		sqlitePullRows.LastUpstreamPullAt.Set(row.LastUpstreamPullAt),
		sqlitePullRows.CreatedAt.Set(row.CreatedAt),
		sqlitePullRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return wrapError(err, "record pull metadata")
	}
	return nil
}
