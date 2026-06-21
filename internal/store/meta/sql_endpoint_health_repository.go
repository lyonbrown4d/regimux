package meta

import (
	"context"
	"errors"

	collectionlist "github.com/arcgolabs/collectionx/list"
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

func (s *SQLStore) PutEndpointHealthSnapshot(ctx context.Context, record EndpointHealthRecord) error {
	_, record, err := normalizeEndpointHealthRecord(record)
	if err != nil {
		return err
	}
	now := metadataNow()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	row, err := s.mapper.EndpointHealthRecordToRow(record)
	if err != nil {
		return err
	}
	return s.upsertEndpointHealthRow(ctx, row)
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
	records, err := mapRows(rows, s.mapper.EndpointHealthRowToRecord)
	if err != nil {
		return nil, err
	}
	filtered := collectionlist.NewList(records...).
		Where(func(_ int, record EndpointHealthRecord) bool {
			return options.Alias == "" || record.Alias == options.Alias
		})
	if options.Limit > 0 {
		filtered = filtered.Take(options.Limit)
	}
	return filtered.Values(), nil
}

func (s *SQLStore) upsertEndpointHealthRow(ctx context.Context, row endpointHealthRow) error {
	id, err := s.generatePullID(ctx)
	if err != nil {
		return err
	}
	row.ID = id
	query, err := s.endpointHealthUpsertSQL()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, query,
		row.ID,
		row.Key,
		row.Alias,
		row.Registry,
		row.Repository,
		row.LatencyEWMA,
		row.LatencySamples,
		row.ConsecutiveFailures,
		row.SuccessCount,
		row.FailureCount,
		row.ContentMismatchCount,
		row.CooldownUntil,
		row.DegradedUntil,
		row.LastSuccessAt,
		row.LastFailureAt,
		row.LastProbeAt,
		row.CreatedAt,
		row.UpdatedAt,
	)
	if err != nil {
		return wrapError(err, "put endpoint health metadata")
	}
	return nil
}

func (s *SQLStore) endpointHealthUpsertSQL() (string, error) {
	switch s.driver {
	case metaDriverMySQL:
		return mysqlEndpointHealthUpsertSQL, nil
	case metaDriverPostgres:
		return postgresEndpointHealthUpsertSQL, nil
	case metaDriverSQLite:
		return sqliteEndpointHealthUpsertSQL, nil
	default:
		return "", errorf("%w: unsupported metadata store driver %q", ErrInvalidValue, s.driver)
	}
}

func (s *SQLStore) updateEndpointHealthRow(ctx context.Context, row endpointHealthRow) error {
	return patchRowByKey(ctx, s.endpointHealth, sqlEndpointHealthRows.Key, row.Key, "upsert endpoint health metadata",
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
}

const mysqlEndpointHealthUpsertSQL = `
INSERT INTO ` + "`meta_endpoint_health`" + ` (` + "`id`" + `, ` + "`key`" + `, ` + "`alias`" + `, ` + "`registry`" + `, ` + "`repository`" + `, ` + "`latency_ewma`" + `, ` + "`latency_samples`" + `, ` + "`consecutive_failures`" + `, ` + "`success_count`" + `, ` + "`failure_count`" + `, ` + "`content_mismatch_count`" + `, ` + "`cooldown_until`" + `, ` + "`degraded_until`" + `, ` + "`last_success_at`" + `, ` + "`last_failure_at`" + `, ` + "`last_probe_at`" + `, ` + "`created_at`" + `, ` + "`updated_at`" + `)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
	` + "`alias`" + ` = VALUES(` + "`alias`" + `),
	` + "`registry`" + ` = VALUES(` + "`registry`" + `),
	` + "`repository`" + ` = VALUES(` + "`repository`" + `),
	` + "`latency_ewma`" + ` = VALUES(` + "`latency_ewma`" + `),
	` + "`latency_samples`" + ` = VALUES(` + "`latency_samples`" + `),
	` + "`consecutive_failures`" + ` = VALUES(` + "`consecutive_failures`" + `),
	` + "`success_count`" + ` = VALUES(` + "`success_count`" + `),
	` + "`failure_count`" + ` = VALUES(` + "`failure_count`" + `),
	` + "`content_mismatch_count`" + ` = VALUES(` + "`content_mismatch_count`" + `),
	` + "`cooldown_until`" + ` = VALUES(` + "`cooldown_until`" + `),
	` + "`degraded_until`" + ` = VALUES(` + "`degraded_until`" + `),
	` + "`last_success_at`" + ` = VALUES(` + "`last_success_at`" + `),
	` + "`last_failure_at`" + ` = VALUES(` + "`last_failure_at`" + `),
	` + "`last_probe_at`" + ` = VALUES(` + "`last_probe_at`" + `),
	` + "`updated_at`" + ` = VALUES(` + "`updated_at`" + `)`

const postgresEndpointHealthUpsertSQL = `
INSERT INTO "meta_endpoint_health" ("id", "key", "alias", "registry", "repository", "latency_ewma", "latency_samples", "consecutive_failures", "success_count", "failure_count", "content_mismatch_count", "cooldown_until", "degraded_until", "last_success_at", "last_failure_at", "last_probe_at", "created_at", "updated_at")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
ON CONFLICT ("key") DO UPDATE SET
	"alias" = EXCLUDED."alias",
	"registry" = EXCLUDED."registry",
	"repository" = EXCLUDED."repository",
	"latency_ewma" = EXCLUDED."latency_ewma",
	"latency_samples" = EXCLUDED."latency_samples",
	"consecutive_failures" = EXCLUDED."consecutive_failures",
	"success_count" = EXCLUDED."success_count",
	"failure_count" = EXCLUDED."failure_count",
	"content_mismatch_count" = EXCLUDED."content_mismatch_count",
	"cooldown_until" = EXCLUDED."cooldown_until",
	"degraded_until" = EXCLUDED."degraded_until",
	"last_success_at" = EXCLUDED."last_success_at",
	"last_failure_at" = EXCLUDED."last_failure_at",
	"last_probe_at" = EXCLUDED."last_probe_at",
	"updated_at" = EXCLUDED."updated_at"`

const sqliteEndpointHealthUpsertSQL = `
INSERT INTO "meta_endpoint_health" ("id", "key", "alias", "registry", "repository", "latency_ewma", "latency_samples", "consecutive_failures", "success_count", "failure_count", "content_mismatch_count", "cooldown_until", "degraded_until", "last_success_at", "last_failure_at", "last_probe_at", "created_at", "updated_at")
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT ("key") DO UPDATE SET
	"alias" = excluded."alias",
	"registry" = excluded."registry",
	"repository" = excluded."repository",
	"latency_ewma" = excluded."latency_ewma",
	"latency_samples" = excluded."latency_samples",
	"consecutive_failures" = excluded."consecutive_failures",
	"success_count" = excluded."success_count",
	"failure_count" = excluded."failure_count",
	"content_mismatch_count" = excluded."content_mismatch_count",
	"cooldown_until" = excluded."cooldown_until",
	"degraded_until" = excluded."degraded_until",
	"last_success_at" = excluded."last_success_at",
	"last_failure_at" = excluded."last_failure_at",
	"last_probe_at" = excluded."last_probe_at",
	"updated_at" = excluded."updated_at"`
