package meta

import (
	"context"
	"errors"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) EndpointHealth(ctx context.Context, key EndpointHealthKey) (*EndpointHealthRecord, bool, error) {
	key, err := normalizeEndpointHealthKey(key)
	if err != nil {
		return nil, false, err
	}
	row, err := repository.By(s.endpointHealth, sqlEndpointHealthRows.Key).Get(ctx, key.String())
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get endpoint health metadata")
	}
	record, err := s.mapper.EndpointHealthRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLStore) UpsertEndpointHealth(ctx context.Context, record EndpointHealthRecord) (*EndpointHealthRecord, error) {
	key, record, err := normalizeEndpointHealthRecord(record)
	if err != nil {
		return nil, err
	}
	now := metadataNow()
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
	row, err := s.mapper.EndpointHealthRecordToRow(record)
	if err != nil {
		return nil, err
	}
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

func (s *SQLStore) ListEndpointHealth(ctx context.Context, opts ...EndpointHealthListOption) ([]EndpointHealthRecord, error) {
	options := endpointHealthListOptions(opts...)
	rows, err := repository.Query(s.endpointHealth).OrderBy(
		sqlEndpointHealthRows.UpdatedAt.Desc(),
		sqlEndpointHealthRows.ID.Desc(),
	).List(ctx)
	if err != nil {
		return nil, wrapError(err, "list endpoint health metadata")
	}

	records := make([]EndpointHealthRecord, 0, rows.Len())
	var decodeErr error
	rows.Range(func(_ int, row endpointHealthRow) bool {
		record, err := s.mapper.EndpointHealthRowToRecord(row)
		if err != nil {
			decodeErr = err
			return false
		}
		if options.Alias != "" && record.Alias != options.Alias {
			return true
		}
		records = append(records, *record)
		return options.Limit <= 0 || len(records) < options.Limit
	})
	if decodeErr != nil {
		return nil, decodeErr
	}
	return records, nil
}

func (s *SQLStore) updateEndpointHealthRow(ctx context.Context, row endpointHealthRow) error {
	_, err := repository.By(s.endpointHealth, sqlEndpointHealthRows.Key).Update(ctx, row.Key,
		sqlEndpointHealthRows.Alias.Set(row.Alias),
		sqlEndpointHealthRows.Registry.Set(row.Registry),
		sqlEndpointHealthRows.Repository.Set(row.Repository),
		sqlEndpointHealthRows.LatencyEWMA.Set(row.LatencyEWMA),
		sqlEndpointHealthRows.LatencySamples.Set(row.LatencySamples),
		sqlEndpointHealthRows.ConsecutiveFailures.Set(row.ConsecutiveFailures),
		sqlEndpointHealthRows.SuccessCount.Set(row.SuccessCount),
		sqlEndpointHealthRows.FailureCount.Set(row.FailureCount),
		sqlEndpointHealthRows.ContentMismatchCount.Set(row.ContentMismatchCount),
		sqlEndpointHealthRows.CooldownUntil.Set(row.CooldownUntil),
		sqlEndpointHealthRows.DegradedUntil.Set(row.DegradedUntil),
		sqlEndpointHealthRows.LastSuccessAt.Set(row.LastSuccessAt),
		sqlEndpointHealthRows.LastFailureAt.Set(row.LastFailureAt),
		sqlEndpointHealthRows.LastProbeAt.Set(row.LastProbeAt),
		sqlEndpointHealthRows.CreatedAt.Set(row.CreatedAt),
		sqlEndpointHealthRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return wrapError(err, "upsert endpoint health metadata")
	}
	return nil
}
