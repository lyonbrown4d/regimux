package meta

import (
	"context"
	"strings"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) CreatePrefetchRun(ctx context.Context, record PrefetchRunRecord) (*PrefetchRunRecord, error) {
	if strings.TrimSpace(record.Status) == "" {
		return nil, errorf("%w: prefetch run status is required", ErrInvalidValue)
	}
	now := metadataNow()
	if record.StartedAt.IsZero() {
		record.StartedAt = now
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	row, err := s.mapper.PrefetchRunRecordToRow(record)
	if err != nil {
		return nil, err
	}
	if err := s.prefetchRuns.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "create prefetch run metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLStore) UpdatePrefetchRun(ctx context.Context, record PrefetchRunRecord) (*PrefetchRunRecord, error) {
	if record.ID == 0 {
		return nil, errorf("%w: prefetch run id is required", ErrInvalidKey)
	}
	if strings.TrimSpace(record.Status) == "" {
		return nil, errorf("%w: prefetch run status is required", ErrInvalidValue)
	}
	record.UpdatedAt = metadataNow()
	row, err := s.mapper.PrefetchRunRecordToRow(record)
	if err != nil {
		return nil, err
	}
	_, err = repository.By(s.prefetchRuns, sqlPrefetchRunRows.ID).Update(ctx, row.ID,
		sqlPrefetchRunRows.Status.Set(row.Status),
		sqlPrefetchRunRows.Trigger.Set(row.Trigger),
		sqlPrefetchRunRows.StartedAt.Set(row.StartedAt),
		sqlPrefetchRunRows.FinishedAt.Set(row.FinishedAt),
		sqlPrefetchRunRows.ScannedRecords.Set(row.ScannedRecords),
		sqlPrefetchRunRows.SkippedRecords.Set(row.SkippedRecords),
		sqlPrefetchRunRows.Repositories.Set(row.Repositories),
		sqlPrefetchRunRows.SkippedRepositories.Set(row.SkippedRepositories),
		sqlPrefetchRunRows.Candidates.Set(row.Candidates),
		sqlPrefetchRunRows.Prefetched.Set(row.Prefetched),
		sqlPrefetchRunRows.Failed.Set(row.Failed),
		sqlPrefetchRunRows.SkippedCandidates.Set(row.SkippedCandidates),
		sqlPrefetchRunRows.BytesWarmed.Set(row.BytesWarmed),
		sqlPrefetchRunRows.ByteBudget.Set(row.ByteBudget),
		sqlPrefetchRunRows.TaskBudget.Set(row.TaskBudget),
		sqlPrefetchRunRows.RepositoryLimit.Set(row.RepositoryLimit),
		sqlPrefetchRunRows.RetryRequested.Set(row.RetryRequested),
		sqlPrefetchRunRows.Error.Set(row.Error),
		sqlPrefetchRunRows.CreatedAt.Set(row.CreatedAt),
		sqlPrefetchRunRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return nil, wrapError(err, "update prefetch run metadata")
	}
	return &record, nil
}

func (s *SQLStore) ListPrefetchRuns(ctx context.Context, opts ...PrefetchRunListOption) ([]PrefetchRunRecord, error) {
	options := prefetchRunListOptions(opts...)
	query := repository.Query(s.prefetchRuns)
	if options.RecentFirst {
		query = query.OrderBy(
			sqlPrefetchRunRows.StartedAt.Desc(),
			sqlPrefetchRunRows.CreatedAt.Desc(),
			sqlPrefetchRunRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list prefetch run metadata")
	}
	return s.prefetchRunRowsToRecords(rows)
}

func (s *SQLStore) CreatePrefetchOutcome(ctx context.Context, record PrefetchOutcomeRecord) (*PrefetchOutcomeRecord, error) {
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
	now := metadataNow()
	if record.StartedAt.IsZero() {
		record.StartedAt = now
	}
	if record.FinishedAt.IsZero() {
		record.FinishedAt = now
	}
	record.CreatedAt = now
	row, err := s.mapper.PrefetchOutcomeRecordToRow(record)
	if err != nil {
		return nil, err
	}
	if err := s.prefetchOutcomes.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "create prefetch outcome metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLStore) LatestPrefetchOutcome(ctx context.Context, key PrefetchCandidateKey) (*PrefetchOutcomeRecord, bool, error) {
	key, err := normalizePrefetchCandidateKey(key)
	if err != nil {
		return nil, false, err
	}
	rows, err := repository.Query(s.prefetchOutcomes).
		Where(sqlPrefetchOutcomeRows.CandidateKey.Eq(key.String())).
		OrderBy(sqlPrefetchOutcomeRows.CreatedAt.Desc(), sqlPrefetchOutcomeRows.ID.Desc()).
		Limit(1).
		List(ctx)
	if err != nil {
		return nil, false, wrapError(err, "get latest prefetch outcome metadata")
	}
	row, ok := firstRow[prefetchOutcomeRow](rows).Get()
	if !ok {
		return nil, false, nil
	}
	record, err := s.mapper.PrefetchOutcomeRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLStore) ListPrefetchOutcomes(ctx context.Context, opts ...PrefetchOutcomeListOption) ([]PrefetchOutcomeRecord, error) {
	options := prefetchOutcomeListOptions(opts...)
	query := repository.Query(s.prefetchOutcomes)
	if options.RunID > 0 {
		query = query.Where(sqlPrefetchOutcomeRows.RunID.Eq(options.RunID))
	}
	if options.RecentFirst {
		query = query.OrderBy(
			sqlPrefetchOutcomeRows.FinishedAt.Desc(),
			sqlPrefetchOutcomeRows.CreatedAt.Desc(),
			sqlPrefetchOutcomeRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list prefetch outcome metadata")
	}
	return s.prefetchOutcomeRowsToRecords(rows)
}

func (s *SQLStore) prefetchRunRowsToRecords(rows interface {
	Values() []prefetchRunRow
}) ([]PrefetchRunRecord, error) {
	return mapRows(rows, s.mapper.PrefetchRunRowToRecord)
}

func (s *SQLStore) prefetchOutcomeRowsToRecords(rows interface {
	Values() []prefetchOutcomeRow
}) ([]PrefetchOutcomeRecord, error) {
	return mapRows(rows, s.mapper.PrefetchOutcomeRowToRecord)
}
