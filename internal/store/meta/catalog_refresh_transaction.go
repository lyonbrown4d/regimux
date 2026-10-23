package meta

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/querydsl"
)

func (s *SQLStore) withCatalogRefreshTx(ctx context.Context, alias string, updates map[string]time.Time) (resultErr error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapError(err, "begin repository catalog refresh")
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, wrapError(err, "rollback repository catalog refresh"))
		}
	}()
	if err := s.refreshRepositoryMetadataTx(ctx, tx, alias, updates); err != nil {
		return err
	}
	if err := tx.CommitContext(ctx); err != nil {
		return wrapError(err, "commit repository catalog refresh")
	}
	committed = true
	return nil
}

func (s *SQLStore) refreshRepositoryMetadataTx(ctx context.Context, tx *dbx.Tx, alias string, updates map[string]time.Time) error {
	if err := s.lockCatalogUpstream(ctx, tx, alias); err != nil {
		return err
	}
	upstream, err := s.loadUpstreamRow(ctx, tx, alias)
	if err != nil {
		return err
	}
	maxAt := time.Time{}
	names := make([]string, 0, len(updates))
	for name, at := range updates {
		names = append(names, name)
		maxAt = maxTime(maxAt, metadataTimestamp(at))
	}
	sort.Strings(names)
	if err := s.ensureCatalogRepositoryRows(ctx, tx, upstream, names, updates); err != nil {
		return err
	}
	for _, name := range names {
		repo, err := s.refreshRepositoryCatalogRow(ctx, tx, alias, name, updates[name])
		if err != nil {
			return err
		}
		maxAt = maxTime(maxAt, repo.UpdatedAt)
	}
	return s.refreshUpstreamMetadata(ctx, tx, upstream.ID, upstream, maxAt)
}

func (s *SQLStore) refreshRepositoryCatalogRow(ctx context.Context, tx *dbx.Tx, alias, name string, at time.Time) (*Repository, error) {
	key := repositoryMetadataKey(alias, name)
	if err := s.lockCatalogRepository(ctx, tx, key); err != nil {
		return nil, err
	}
	row, err := s.loadRepositoryRow(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	repo, err := s.mapper.RepositoryRowToRecord(row)
	if err != nil {
		return nil, err
	}
	stats, err := s.repositoryAggregate(ctx, tx, repo.Alias, repo.Name)
	if err != nil {
		return nil, err
	}
	applyRepositoryAggregate(repo, stats, at)
	row, err = s.mapper.RepositoryRecordToRow(*repo)
	if err != nil {
		return nil, err
	}
	if err := s.updateRepositoryRow(ctx, tx, row); err != nil {
		return nil, err
	}
	return repo, nil
}

func (s *SQLStore) lockCatalogRepository(ctx context.Context, tx *dbx.Tx, key string) error {
	if s.driver == metaDriverSQLite {
		return nil
	}
	query := `SELECT "id" FROM "meta_repositories" WHERE "key" = $1 FOR UPDATE`
	if s.driver == metaDriverMySQL {
		query = "SELECT `id` FROM `meta_repositories` WHERE `key` = ? FOR UPDATE"
	}
	var id int64
	if err := tx.QueryRowContext(ctx, query, key).Scan(&id); err != nil {
		return wrapError(err, "lock repository catalog refresh")
	}
	return nil
}

func applyRepositoryAggregate(repo *Repository, stats repositoryAggregate, at time.Time) {
	repo.PullCount = stats.PullCount
	repo.PolicyDeniedPullCount = stats.PolicyDeniedPullCount
	repo.BlobBytes = stats.BlobBytes
	repo.BlobLinkCount = stats.BlobLinkCount
	repo.LastPullAt = stats.LastPullAt
	repo.LastPolicyDeniedAt = stats.LastPolicyDeniedAt
	repo.LastBlobAccessAt = stats.LastBlobAccessAt
	repo.LastActivityAt = stats.LastActivityAt
	repo.UpdatedAt = maxTime(metadataTimestamp(at), repo.UpdatedAt, stats.LastActivityAt)
}

func (s *SQLStore) lockCatalogUpstream(ctx context.Context, tx *dbx.Tx, alias string) error {
	if s.driver == metaDriverSQLite {
		if _, err := dbx.Exec(ctx, tx, querydsl.Update(sqlUpstreamRows).
			Set(sqlUpstreamRows.ID.SetColumn(sqlUpstreamRows.ID)).
			Where(sqlUpstreamRows.Alias.Eq(alias))); err != nil {
			return wrapError(err, "lock upstream catalog refresh")
		}
		return nil
	}

	query := `SELECT "id" FROM "meta_upstreams" WHERE "alias" = $1 FOR UPDATE`
	if s.driver == metaDriverMySQL {
		query = "SELECT `id` FROM `meta_upstreams` WHERE `alias` = ? FOR UPDATE"
	}
	var id int64
	if err := tx.QueryRowContext(ctx, query, alias).Scan(&id); err != nil {
		return wrapError(err, "lock upstream catalog refresh")
	}
	return nil
}

func (s *SQLStore) loadUpstreamRow(ctx context.Context, session dbx.Session, alias string) (upstreamRow, error) {
	row, err := dbx.GetTyped[upstreamRow](ctx, session, querydsl.SelectInto[upstreamRow](
		querydsl.AllColumns(sqlUpstreamRows).Values()...,
	).From(sqlUpstreamRows).Where(sqlUpstreamRows.Alias.Eq(alias)))
	if err != nil {
		return upstreamRow{}, wrapError(err, "read locked upstream catalog row")
	}
	return row, nil
}

func (s *SQLStore) loadRepositoryRow(ctx context.Context, session dbx.Session, key string) (repositoryRow, error) {
	row, err := dbx.GetTyped[repositoryRow](ctx, session, querydsl.SelectInto[repositoryRow](
		querydsl.AllColumns(sqlRepositoryRows).Values()...,
	).From(sqlRepositoryRows).Where(sqlRepositoryRows.Key.Eq(key)))
	if err != nil {
		return repositoryRow{}, wrapError(err, "read locked repository catalog row")
	}
	return row, nil
}
