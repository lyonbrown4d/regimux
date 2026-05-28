package meta

import (
	"context"
	"errors"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLiteStore) EndpointHealth(ctx context.Context, key EndpointHealthKey) (*EndpointHealthRecord, bool, error) {
	key, err := normalizeEndpointHealthKey(key)
	if err != nil {
		return nil, false, err
	}
	row, err := repository.By(s.endpointHealth, sqliteEndpointHealthRows.Key).Get(ctx, key.String())
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get endpoint health metadata")
	}
	return endpointHealthRowToRecord(row), true, nil
}

func (s *SQLiteStore) UpsertEndpointHealth(ctx context.Context, record EndpointHealthRecord) (*EndpointHealthRecord, error) {
	key, record, err := normalizeEndpointHealthRecord(record)
	if err != nil {
		return nil, err
	}
	now := sqliteNow()
	existing, ok, err := s.EndpointHealth(ctx, key)
	if err != nil {
		return nil, err
	}
	if ok {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	row := endpointHealthRecordToRow(record)
	if record.ID != 0 {
		if err := s.updateEndpointHealthRow(ctx, row); err != nil {
			return nil, err
		}
		return &record, nil
	}
	if err := s.endpointHealth.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "upsert endpoint health metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLiteStore) ListEndpointHealth(ctx context.Context, opts ...EndpointHealthListOption) ([]EndpointHealthRecord, error) {
	options := endpointHealthListOptions(opts...)
	rows, err := repository.Query(s.endpointHealth).OrderBy(
		sqliteEndpointHealthRows.UpdatedAt.Desc(),
		sqliteEndpointHealthRows.ID.Desc(),
	).List(ctx)
	if err != nil {
		return nil, wrapError(err, "list endpoint health metadata")
	}

	records := make([]EndpointHealthRecord, 0, rows.Len())
	rows.Range(func(_ int, row endpointHealthRow) bool {
		record := endpointHealthRowToRecord(row)
		if options.Alias != "" && record.Alias != options.Alias {
			return true
		}
		records = append(records, *record)
		return options.Limit <= 0 || len(records) < options.Limit
	})
	return records, nil
}

func (s *SQLiteStore) updateEndpointHealthRow(ctx context.Context, row endpointHealthRow) error {
	_, err := repository.By(s.endpointHealth, sqliteEndpointHealthRows.Key).Update(ctx, row.Key,
		sqliteEndpointHealthRows.Alias.Set(row.Alias),
		sqliteEndpointHealthRows.Registry.Set(row.Registry),
		sqliteEndpointHealthRows.Repository.Set(row.Repository),
		sqliteEndpointHealthRows.LatencyEWMA.Set(row.LatencyEWMA),
		sqliteEndpointHealthRows.LatencySamples.Set(row.LatencySamples),
		sqliteEndpointHealthRows.ConsecutiveFailures.Set(row.ConsecutiveFailures),
		sqliteEndpointHealthRows.SuccessCount.Set(row.SuccessCount),
		sqliteEndpointHealthRows.FailureCount.Set(row.FailureCount),
		sqliteEndpointHealthRows.ContentMismatchCount.Set(row.ContentMismatchCount),
		sqliteEndpointHealthRows.CooldownUntil.Set(row.CooldownUntil),
		sqliteEndpointHealthRows.DegradedUntil.Set(row.DegradedUntil),
		sqliteEndpointHealthRows.LastSuccessAt.Set(row.LastSuccessAt),
		sqliteEndpointHealthRows.LastFailureAt.Set(row.LastFailureAt),
		sqliteEndpointHealthRows.LastProbeAt.Set(row.LastProbeAt),
		sqliteEndpointHealthRows.CreatedAt.Set(row.CreatedAt),
		sqliteEndpointHealthRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return wrapError(err, "upsert endpoint health metadata")
	}
	return nil
}
