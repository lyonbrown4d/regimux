package meta

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/querydsl"
)

func (s *SQLStore) withCatalogRefreshTx(ctx context.Context, seed *Repository, at time.Time) (resultErr error) {
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
	if err := s.refreshRepositoryMetadataTx(ctx, tx, seed, at); err != nil {
		return err
	}
	if err := tx.CommitContext(ctx); err != nil {
		return wrapError(err, "commit repository catalog refresh")
	}
	committed = true
	return nil
}

func (s *SQLStore) refreshRepositoryMetadataTx(ctx context.Context, tx *dbx.Tx, seed *Repository, at time.Time) error {
	if err := s.lockCatalogUpstream(ctx, tx, seed.Alias); err != nil {
		return err
	}
	upstreamRow, err := s.loadUpstreamRow(ctx, tx, seed.Alias)
	if err != nil {
		return err
	}
	repo, err := s.refreshRepositoryCatalogRow(ctx, tx, seed.Alias, seed.Name, at)
	if err != nil {
		return err
	}
	return s.refreshUpstreamMetadata(ctx, tx, upstreamRow.ID, upstreamRow, repo.UpdatedAt)
}

func (s *SQLStore) refreshRepositoryCatalogRow(ctx context.Context, tx *dbx.Tx, alias, name string, at time.Time) (*Repository, error) {
	key := repositoryMetadataKey(alias, name)
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
