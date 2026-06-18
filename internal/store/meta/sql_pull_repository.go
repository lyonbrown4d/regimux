package meta

import (
	"context"
	"errors"
	"time"

	"github.com/arcgolabs/dbx/idgen"
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
	now := metadataTimestamp(at)
	record := PullRecord{
		Key:        key.String(),
		Alias:      key.Alias,
		Repository: key.Repository,
		Reference:  key.Reference,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	kindErr := applyPullRecordKind(&record, kind, now)
	if kindErr != nil {
		return nil, kindErr
	}
	row, err := s.mapper.PullRecordToRow(record)
	if err != nil {
		return nil, err
	}
	if upsertErr := s.upsertPullRow(ctx, row); upsertErr != nil {
		return nil, upsertErr
	}
	if refreshErr := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, now); refreshErr != nil {
		return nil, refreshErr
	}
	updated, ok, err := s.Pull(ctx, key)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errorf("record pull metadata: %w", ErrNotFound)
	}
	return updated, nil
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

func applyPullRecordKind(record *PullRecord, kind pullRecordKind, now time.Time) error {
	switch kind {
	case pullRecordClient:
		record.Count++
		record.LastPullAt = now
	case pullRecordUpstream:
		record.LastUpstreamPullAt = now
	case pullRecordPolicyDenied:
		record.PolicyDeniedCount++
		record.LastPolicyDeniedAt = now
	default:
		return errorf("unknown pull record kind: %d", kind)
	}
	return nil
}

func (s *SQLStore) upsertPullRow(ctx context.Context, row pullRow) error {
	id, err := s.generatePullID(ctx)
	if err != nil {
		return err
	}
	row.ID = id
	query, err := s.pullUpsertSQL()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, query,
		row.ID,
		row.Key,
		row.Alias,
		row.Repository,
		row.Reference,
		row.Count,
		row.PolicyDeniedCount,
		row.LastPullAt,
		row.LastUpstreamPullAt,
		row.LastPolicyDeniedAt,
		row.CreatedAt,
		row.UpdatedAt,
	)
	if err != nil {
		return wrapError(err, "record pull metadata")
	}
	return nil
}

func (s *SQLStore) generatePullID(ctx context.Context) (int64, error) {
	generator := s.db.IDGenerator()
	if generator == nil {
		return 0, wrapError(ErrInvalidValue, "generate pull metadata id")
	}
	value, err := generator.GenerateID(ctx, idgen.Request{Strategy: idgen.StrategySnowflake})
	if err != nil {
		return 0, wrapError(err, "generate pull metadata id")
	}
	id, ok := value.(int64)
	if !ok || id <= 0 {
		return 0, errorf("%w: generated pull metadata id has type %T and value %v", ErrInvalidValue, value, value)
	}
	return id, nil
}

func (s *SQLStore) pullUpsertSQL() (string, error) {
	switch s.driver {
	case metaDriverMySQL:
		return mysqlPullUpsertSQL, nil
	case metaDriverPostgres:
		return postgresPullUpsertSQL, nil
	case metaDriverSQLite:
		return sqlitePullUpsertSQL, nil
	default:
		return "", errorf("%w: unsupported metadata store driver %q", ErrInvalidValue, s.driver)
	}
}

const mysqlPullUpsertSQL = `
INSERT INTO ` + "`meta_pulls`" + ` (` + "`id`" + `, ` + "`key`" + `, ` + "`alias`" + `, ` + "`repository`" + `, ` + "`reference`" + `, ` + "`count`" + `, ` + "`policy_denied_count`" + `, ` + "`last_pull_at`" + `, ` + "`last_upstream_pull_at`" + `, ` + "`last_policy_denied_at`" + `, ` + "`created_at`" + `, ` + "`updated_at`" + `)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
	` + "`alias`" + ` = VALUES(` + "`alias`" + `),
	` + "`repository`" + ` = VALUES(` + "`repository`" + `),
	` + "`reference`" + ` = VALUES(` + "`reference`" + `),
	` + "`count`" + ` = ` + "`count`" + ` + VALUES(` + "`count`" + `),
	` + "`policy_denied_count`" + ` = ` + "`policy_denied_count`" + ` + VALUES(` + "`policy_denied_count`" + `),
	` + "`last_pull_at`" + ` = GREATEST(` + "`last_pull_at`" + `, VALUES(` + "`last_pull_at`" + `)),
	` + "`last_upstream_pull_at`" + ` = GREATEST(` + "`last_upstream_pull_at`" + `, VALUES(` + "`last_upstream_pull_at`" + `)),
	` + "`last_policy_denied_at`" + ` = GREATEST(` + "`last_policy_denied_at`" + `, VALUES(` + "`last_policy_denied_at`" + `)),
	` + "`updated_at`" + ` = GREATEST(` + "`updated_at`" + `, VALUES(` + "`updated_at`" + `))`

const postgresPullUpsertSQL = `
INSERT INTO "meta_pulls" ("id", "key", "alias", "repository", "reference", "count", "policy_denied_count", "last_pull_at", "last_upstream_pull_at", "last_policy_denied_at", "created_at", "updated_at")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT ("key") DO UPDATE SET
	"alias" = EXCLUDED."alias",
	"repository" = EXCLUDED."repository",
	"reference" = EXCLUDED."reference",
	"count" = "meta_pulls"."count" + EXCLUDED."count",
	"policy_denied_count" = "meta_pulls"."policy_denied_count" + EXCLUDED."policy_denied_count",
	"last_pull_at" = GREATEST("meta_pulls"."last_pull_at", EXCLUDED."last_pull_at"),
	"last_upstream_pull_at" = GREATEST("meta_pulls"."last_upstream_pull_at", EXCLUDED."last_upstream_pull_at"),
	"last_policy_denied_at" = GREATEST("meta_pulls"."last_policy_denied_at", EXCLUDED."last_policy_denied_at"),
	"updated_at" = GREATEST("meta_pulls"."updated_at", EXCLUDED."updated_at")`

const sqlitePullUpsertSQL = `
INSERT INTO "meta_pulls" ("id", "key", "alias", "repository", "reference", "count", "policy_denied_count", "last_pull_at", "last_upstream_pull_at", "last_policy_denied_at", "created_at", "updated_at")
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT ("key") DO UPDATE SET
	"alias" = excluded."alias",
	"repository" = excluded."repository",
	"reference" = excluded."reference",
	"count" = "meta_pulls"."count" + excluded."count",
	"policy_denied_count" = "meta_pulls"."policy_denied_count" + excluded."policy_denied_count",
	"last_pull_at" = max("meta_pulls"."last_pull_at", excluded."last_pull_at"),
	"last_upstream_pull_at" = max("meta_pulls"."last_upstream_pull_at", excluded."last_upstream_pull_at"),
	"last_policy_denied_at" = max("meta_pulls"."last_policy_denied_at", excluded."last_policy_denied_at"),
	"updated_at" = max("meta_pulls"."updated_at", excluded."updated_at")`
