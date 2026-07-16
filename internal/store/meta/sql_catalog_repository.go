package meta

import (
	"context"
	"errors"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dbx/querydsl"
	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) UpstreamByAlias(ctx context.Context, alias string) (*Upstream, error) {
	alias, err := normalizeUpstreamAlias(alias)
	if err != nil {
		return nil, err
	}
	row, err := repository.By(s.upstreams, sqlUpstreamRows.Alias).Get(ctx, alias)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, wrapError(err, "get upstream metadata")
	}
	record, err := s.mapper.UpstreamRowToRecord(row)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func (s *SQLStore) ListUpstreams(ctx context.Context, opts ...UpstreamListOption) (*collectionlist.List[Upstream], error) {
	options := upstreamListOptions(opts...)
	query := repository.Query(s.upstreams)
	if options.RecentFirst {
		query = query.OrderBy(
			sqlUpstreamRows.LastActivityAt.Desc(),
			sqlUpstreamRows.UpdatedAt.Desc(),
			sqlUpstreamRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list upstream metadata")
	}
	return s.upstreamRowsToRecords(rows)
}

func (s *SQLStore) RepositoryByName(ctx context.Context, upstreamID int64, name string) (*Repository, error) {
	if upstreamID <= 0 {
		return nil, errorf("%w: upstream id is required", ErrInvalidKey)
	}
	name, err := normalizeRepositoryName(name)
	if err != nil {
		return nil, err
	}
	row, err := repository.Query(s.repositories).
		Where(querydsl.And(
			sqlRepositoryRows.UpstreamID.Eq(upstreamID),
			sqlRepositoryRows.Name.Eq(name),
		)).
		First(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, wrapError(err, "get repository metadata")
	}
	record, err := s.mapper.RepositoryRowToRecord(row)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func (s *SQLStore) ListRepositories(ctx context.Context, opts ...RepositoryListOption) (*collectionlist.List[Repository], error) {
	options := repositoryListOptions(opts...)
	query := repository.Query(s.repositories)
	if options.RecentFirst {
		query = query.OrderBy(
			sqlRepositoryRows.LastActivityAt.Desc(),
			sqlRepositoryRows.UpdatedAt.Desc(),
			sqlRepositoryRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list repository metadata")
	}
	return s.repositoryRowsToRecords(rows)
}

func (s *SQLStore) upstreamRowsToRecords(rows rowCollection[upstreamRow]) (*collectionlist.List[Upstream], error) {
	return mapRows(rows, s.mapper.UpstreamRowToRecord)
}

func (s *SQLStore) repositoryRowsToRecords(rows rowCollection[repositoryRow]) (*collectionlist.List[Repository], error) {
	return mapRows(rows, s.mapper.RepositoryRowToRecord)
}
