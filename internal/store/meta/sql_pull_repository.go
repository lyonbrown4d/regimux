package meta

import (
	"context"
	"errors"
	"time"

	"github.com/arcgolabs/dbx/paging"
	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) Pull(ctx context.Context, key PullKey) (*PullRecord, bool, error) {
	key, err := normalizePullKey(key)
	if err != nil {
		return nil, false, err
	}
	row, err := repository.By(s.pulls, sqlPullRows.Key).Get(ctx, key.String())
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get pull metadata")
	}
	record, err := s.mapper.PullRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLStore) RecordPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error) {
	return s.recordPull(ctx, key, at, pullRecordClient)
}

func (s *SQLStore) RecordUpstreamPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error) {
	return s.recordPull(ctx, key, at, pullRecordUpstream)
}

func (s *SQLStore) RecordPolicyDeniedPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error) {
	return s.recordPull(ctx, key, at, pullRecordPolicyDenied)
}

type pullRecordKind int

const (
	pullRecordClient pullRecordKind = iota
	pullRecordUpstream
	pullRecordPolicyDenied
)

func (s *SQLStore) recordPull(ctx context.Context, key PullKey, at time.Time, kind pullRecordKind) (*PullRecord, error) {
	key, err := normalizePullKey(key)
	if err != nil {
		return nil, err
	}
	now := at.UTC()
	if now.IsZero() {
		now = metadataNow()
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
	switch kind {
	case pullRecordUpstream:
		record.LastUpstreamPullAt = now
	case pullRecordPolicyDenied:
		record.PolicyDeniedCount++
		record.LastPolicyDeniedAt = now
	default:
		record.Count++
		record.LastPullAt = now
	}
	row, err := s.mapper.PullRecordToRow(record)
	if err != nil {
		return nil, err
	}
	if err := s.writePullRecord(ctx, key, &record, row, now); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLStore) writePullRecord(ctx context.Context, key PullKey, record *PullRecord, row pullRow, at time.Time) error {
	if record.ID != 0 {
		if err := s.updatePullRow(ctx, row); err != nil {
			return err
		}
		return s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, at)
	}
	if err := s.pulls.Create(ctx, &row); err != nil {
		return wrapError(err, "record pull metadata")
	}
	record.ID = row.ID
	return s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, at)
}

func (s *SQLStore) ListPulls(ctx context.Context, opts ...PullListOption) ([]PullRecord, error) {
	options := pullListOptions(opts...)
	query := repository.Query(s.pulls)
	if options.RecentFirst {
		query = query.OrderBy(
			sqlPullRows.LastPullAt.Desc(),
			sqlPullRows.LastPolicyDeniedAt.Desc(),
			sqlPullRows.UpdatedAt.Desc(),
			sqlPullRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		page, err := query.ListPage(ctx, paging.NewRequest(1, options.Limit))
		if err != nil {
			return nil, wrapError(err, "list pull metadata")
		}
		return s.pullRowsToRecords(page.Items)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list pull metadata")
	}
	return s.pullRowsToRecords(rows)
}

func (s *SQLStore) pullRowsToRecords(rows interface {
	Values() []pullRow
}) ([]PullRecord, error) {
	return mapRows(rows, s.mapper.PullRowToRecord)
}

func (s *SQLStore) updatePullRow(ctx context.Context, row pullRow) error {
	return patchRowByKey(ctx, s.pulls, sqlPullRows.Key, row.Key, "record pull metadata",
		sqlPullRows.Alias.Set(row.Alias),
		sqlPullRows.Repository.Set(row.Repository),
		sqlPullRows.Reference.Set(row.Reference),
		sqlPullRows.Count.Set(row.Count),
		sqlPullRows.PolicyDeniedCount.Set(row.PolicyDeniedCount),
		sqlPullRows.LastPullAt.Set(row.LastPullAt),
		sqlPullRows.LastUpstreamPullAt.Set(row.LastUpstreamPullAt),
		sqlPullRows.LastPolicyDeniedAt.Set(row.LastPolicyDeniedAt),
		sqlPullRows.CreatedAt.Set(row.CreatedAt),
		sqlPullRows.UpdatedAt.Set(row.UpdatedAt),
	)
}
