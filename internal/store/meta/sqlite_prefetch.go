package meta

import (
	"context"
	"strings"
	"time"

	"github.com/arcgolabs/dbx/querydsl"
	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLiteStore) CreatePrefetchRun(ctx context.Context, record PrefetchRunRecord) (*PrefetchRunRecord, error) {
	if strings.TrimSpace(record.Status) == "" {
		return nil, errorf("%w: prefetch run status is required", ErrInvalidValue)
	}
	now := sqliteNow()
	if record.StartedAt.IsZero() {
		record.StartedAt = now
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	row := prefetchRunRecordToRow(record)
	if err := s.prefetchRuns.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "create prefetch run metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLiteStore) UpdatePrefetchRun(ctx context.Context, record PrefetchRunRecord) (*PrefetchRunRecord, error) {
	if record.ID == 0 {
		return nil, errorf("%w: prefetch run id is required", ErrInvalidKey)
	}
	if strings.TrimSpace(record.Status) == "" {
		return nil, errorf("%w: prefetch run status is required", ErrInvalidValue)
	}
	record.UpdatedAt = sqliteNow()
	row := prefetchRunRecordToRow(record)
	_, err := repository.By(s.prefetchRuns, sqlitePrefetchRunRows.ID).Update(ctx, row.ID,
		sqlitePrefetchRunRows.Status.Set(row.Status),
		sqlitePrefetchRunRows.Trigger.Set(row.Trigger),
		sqlitePrefetchRunRows.StartedAt.Set(row.StartedAt),
		sqlitePrefetchRunRows.FinishedAt.Set(row.FinishedAt),
		sqlitePrefetchRunRows.ScannedRecords.Set(row.ScannedRecords),
		sqlitePrefetchRunRows.SkippedRecords.Set(row.SkippedRecords),
		sqlitePrefetchRunRows.Repositories.Set(row.Repositories),
		sqlitePrefetchRunRows.SkippedRepositories.Set(row.SkippedRepositories),
		sqlitePrefetchRunRows.Candidates.Set(row.Candidates),
		sqlitePrefetchRunRows.Prefetched.Set(row.Prefetched),
		sqlitePrefetchRunRows.Failed.Set(row.Failed),
		sqlitePrefetchRunRows.SkippedCandidates.Set(row.SkippedCandidates),
		sqlitePrefetchRunRows.BytesWarmed.Set(row.BytesWarmed),
		sqlitePrefetchRunRows.ByteBudget.Set(row.ByteBudget),
		sqlitePrefetchRunRows.TaskBudget.Set(row.TaskBudget),
		sqlitePrefetchRunRows.RepositoryLimit.Set(row.RepositoryLimit),
		sqlitePrefetchRunRows.RetryRequested.Set(row.RetryRequested),
		sqlitePrefetchRunRows.Error.Set(row.Error),
		sqlitePrefetchRunRows.CreatedAt.Set(row.CreatedAt),
		sqlitePrefetchRunRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return nil, wrapError(err, "update prefetch run metadata")
	}
	return &record, nil
}

func (s *SQLiteStore) ListPrefetchRuns(ctx context.Context, opts ...PrefetchRunListOption) ([]PrefetchRunRecord, error) {
	options := prefetchRunListOptions(opts...)
	query := repository.Query(s.prefetchRuns)
	if options.RecentFirst {
		query = query.OrderBy(
			sqlitePrefetchRunRows.StartedAt.Desc(),
			sqlitePrefetchRunRows.CreatedAt.Desc(),
			sqlitePrefetchRunRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list prefetch run metadata")
	}
	return prefetchRunRowsToRecords(rows), nil
}

func (s *SQLiteStore) CreatePrefetchOutcome(ctx context.Context, record PrefetchOutcomeRecord) (*PrefetchOutcomeRecord, error) {
	_, record, err := normalizePrefetchOutcomeRecord(record)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(record.Status) == "" {
		return nil, errorf("%w: prefetch outcome status is required", ErrInvalidValue)
	}
	if record.Attempt <= 0 {
		record.Attempt = 1
	}
	now := sqliteNow()
	if record.StartedAt.IsZero() {
		record.StartedAt = now
	}
	if record.FinishedAt.IsZero() {
		record.FinishedAt = now
	}
	record.CreatedAt = now
	row := prefetchOutcomeRecordToRow(record)
	if err := s.prefetchOutcomes.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "create prefetch outcome metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLiteStore) LatestPrefetchOutcome(ctx context.Context, key PrefetchCandidateKey) (*PrefetchOutcomeRecord, bool, error) {
	key, err := normalizePrefetchCandidateKey(key)
	if err != nil {
		return nil, false, err
	}
	rows, err := repository.Query(s.prefetchOutcomes).
		Where(sqlitePrefetchOutcomeRows.CandidateKey.Eq(key.String())).
		OrderBy(sqlitePrefetchOutcomeRows.CreatedAt.Desc(), sqlitePrefetchOutcomeRows.ID.Desc()).
		Limit(1).
		List(ctx)
	if err != nil {
		return nil, false, wrapError(err, "get latest prefetch outcome metadata")
	}
	if rows.Len() == 0 {
		return nil, false, nil
	}
	return prefetchOutcomeRowToRecord(rows.Values()[0]), true, nil
}

func (s *SQLiteStore) ListPrefetchOutcomes(ctx context.Context, opts ...PrefetchOutcomeListOption) ([]PrefetchOutcomeRecord, error) {
	options := prefetchOutcomeListOptions(opts...)
	query := repository.Query(s.prefetchOutcomes)
	if options.RunID > 0 {
		query = query.Where(sqlitePrefetchOutcomeRows.RunID.Eq(options.RunID))
	}
	if options.RecentFirst {
		query = query.OrderBy(
			sqlitePrefetchOutcomeRows.FinishedAt.Desc(),
			sqlitePrefetchOutcomeRows.CreatedAt.Desc(),
			sqlitePrefetchOutcomeRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list prefetch outcome metadata")
	}
	return prefetchOutcomeRowsToRecords(rows), nil
}

func (s *SQLiteStore) RequestPrefetchControl(ctx context.Context, record PrefetchControlRecord) (*PrefetchControlRecord, error) {
	record.Action = strings.ToLower(strings.TrimSpace(record.Action))
	if record.Action == "" {
		return nil, errorf("%w: prefetch control action is required", ErrInvalidValue)
	}
	now := sqliteNow()
	if record.RequestedAt.IsZero() {
		record.RequestedAt = now
	}
	record.CreatedAt = now
	record.UpdatedAt = now
	row := prefetchControlRecordToRow(record)
	if err := s.prefetchControls.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "create prefetch control metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLiteStore) ConsumePrefetchControl(ctx context.Context, action string, at time.Time) (*PrefetchControlRecord, bool, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return nil, false, errorf("%w: prefetch control action is required", ErrInvalidValue)
	}
	if at.IsZero() {
		at = sqliteNow()
	}
	rows, err := repository.Query(s.prefetchControls).
		Where(querydsl.And(
			sqlitePrefetchControlRows.Action.Eq(action),
			sqlitePrefetchControlRows.ConsumedAt.Eq(0),
		)).
		OrderBy(sqlitePrefetchControlRows.RequestedAt.Asc(), sqlitePrefetchControlRows.ID.Asc()).
		Limit(1).
		List(ctx)
	if err != nil {
		return nil, false, wrapError(err, "find prefetch control metadata")
	}
	if rows.Len() == 0 {
		return nil, false, nil
	}
	record := prefetchControlRowToRecord(rows.Values()[0])
	record.ConsumedAt = at.UTC()
	record.UpdatedAt = sqliteNow()
	row := prefetchControlRecordToRow(*record)
	_, err = repository.By(s.prefetchControls, sqlitePrefetchControlRows.ID).Update(ctx, row.ID,
		sqlitePrefetchControlRows.ConsumedAt.Set(row.ConsumedAt),
		sqlitePrefetchControlRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return nil, false, wrapError(err, "consume prefetch control metadata")
	}
	return record, true, nil
}

func (s *SQLiteStore) ListPrefetchControls(ctx context.Context, opts ...PrefetchControlListOption) ([]PrefetchControlRecord, error) {
	options := prefetchControlListOptions(opts...)
	query := repository.Query(s.prefetchControls)
	if options.RecentFirst {
		query = query.OrderBy(
			sqlitePrefetchControlRows.RequestedAt.Desc(),
			sqlitePrefetchControlRows.CreatedAt.Desc(),
			sqlitePrefetchControlRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list prefetch control metadata")
	}
	return prefetchControlRowsToRecords(rows), nil
}

func prefetchRunRowsToRecords(rows interface {
	Len() int
	Range(func(int, prefetchRunRow) bool)
}) []PrefetchRunRecord {
	records := make([]PrefetchRunRecord, 0, rows.Len())
	rows.Range(func(_ int, row prefetchRunRow) bool {
		records = append(records, *prefetchRunRowToRecord(row))
		return true
	})
	return records
}

func prefetchOutcomeRowsToRecords(rows interface {
	Len() int
	Range(func(int, prefetchOutcomeRow) bool)
}) []PrefetchOutcomeRecord {
	records := make([]PrefetchOutcomeRecord, 0, rows.Len())
	rows.Range(func(_ int, row prefetchOutcomeRow) bool {
		records = append(records, *prefetchOutcomeRowToRecord(row))
		return true
	})
	return records
}

func prefetchControlRowsToRecords(rows interface {
	Len() int
	Range(func(int, prefetchControlRow) bool)
}) []PrefetchControlRecord {
	records := make([]PrefetchControlRecord, 0, rows.Len())
	rows.Range(func(_ int, row prefetchControlRow) bool {
		records = append(records, *prefetchControlRowToRecord(row))
		return true
	})
	return records
}
