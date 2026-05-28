package meta

import (
	"context"
	"errors"
	"time"

	"github.com/arcgolabs/dbx/querydsl"
	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLiteStore) UpstreamByAlias(ctx context.Context, alias string) (*Upstream, error) {
	alias, err := normalizeUpstreamAlias(alias)
	if err != nil {
		return nil, err
	}
	row, err := repository.By(s.upstreams, sqliteUpstreamRows.Alias).Get(ctx, alias)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, wrapError(err, "get upstream metadata")
	}
	return upstreamRowToRecord(row), nil
}

func (s *SQLiteStore) ListUpstreams(ctx context.Context, opts ...UpstreamListOption) ([]Upstream, error) {
	options := upstreamListOptions(opts...)
	query := repository.Query(s.upstreams)
	if options.RecentFirst {
		query = query.OrderBy(
			sqliteUpstreamRows.LastActivityAt.Desc(),
			sqliteUpstreamRows.UpdatedAt.Desc(),
			sqliteUpstreamRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list upstream metadata")
	}
	return upstreamRowsToRecords(rows), nil
}

func (s *SQLiteStore) RepositoryByName(ctx context.Context, upstreamID int64, name string) (*Repository, error) {
	if upstreamID <= 0 {
		return nil, errorf("%w: upstream id is required", ErrInvalidKey)
	}
	name, err := normalizeRepositoryName(name)
	if err != nil {
		return nil, err
	}
	row, err := repository.Query(s.repositories).
		Where(querydsl.And(
			sqliteRepositoryRows.UpstreamID.Eq(upstreamID),
			sqliteRepositoryRows.Name.Eq(name),
		)).
		First(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, wrapError(err, "get repository metadata")
	}
	return repositoryRowToRecord(row), nil
}

func (s *SQLiteStore) ListRepositories(ctx context.Context, opts ...RepositoryListOption) ([]Repository, error) {
	options := repositoryListOptions(opts...)
	query := repository.Query(s.repositories)
	if options.RecentFirst {
		query = query.OrderBy(
			sqliteRepositoryRows.LastActivityAt.Desc(),
			sqliteRepositoryRows.UpdatedAt.Desc(),
			sqliteRepositoryRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list repository metadata")
	}
	return repositoryRowsToRecords(rows), nil
}

func upstreamRowsToRecords(rows interface {
	Len() int
	Range(func(int, upstreamRow) bool)
}) []Upstream {
	records := make([]Upstream, 0, rows.Len())
	rows.Range(func(_ int, row upstreamRow) bool {
		records = append(records, *upstreamRowToRecord(row))
		return true
	})
	return records
}

func repositoryRowsToRecords(rows interface {
	Len() int
	Range(func(int, repositoryRow) bool)
}) []Repository {
	records := make([]Repository, 0, rows.Len())
	rows.Range(func(_ int, row repositoryRow) bool {
		records = append(records, *repositoryRowToRecord(row))
		return true
	})
	return records
}

func (s *SQLiteStore) ensureRepositoryMetadata(ctx context.Context, alias, name string, at time.Time) (*Repository, error) {
	upstream, err := s.ensureUpstreamMetadata(ctx, alias, at)
	if err != nil {
		return nil, err
	}
	name, err = normalizeRepositoryName(name)
	if err != nil {
		return nil, err
	}
	key := repositoryMetadataKey(upstream.Alias, name)
	row, err := repository.By(s.repositories, sqliteRepositoryRows.Key).Get(ctx, key)
	if err == nil {
		return repositoryRowToRecord(row), nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, wrapError(err, "get repository metadata")
	}
	now := metadataTimestamp(at)
	record := Repository{
		UpstreamID: upstream.ID,
		Alias:      upstream.Alias,
		Name:       name,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	row = repositoryRecordToRow(record)
	if err := s.repositories.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "create repository metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLiteStore) ensureUpstreamMetadata(ctx context.Context, alias string, at time.Time) (*Upstream, error) {
	alias, err := normalizeUpstreamAlias(alias)
	if err != nil {
		return nil, err
	}
	row, err := repository.By(s.upstreams, sqliteUpstreamRows.Alias).Get(ctx, alias)
	if err == nil {
		return upstreamRowToRecord(row), nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, wrapError(err, "get upstream metadata")
	}
	now := metadataTimestamp(at)
	record := Upstream{
		Alias:     alias,
		CreatedAt: now,
		UpdatedAt: now,
	}
	row = upstreamRecordToRow(record)
	if err := s.upstreams.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "create upstream metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLiteStore) refreshRepositoryMetadata(ctx context.Context, alias, name string, at time.Time) error {
	repo, err := s.ensureRepositoryMetadata(ctx, alias, name, at)
	if err != nil {
		return err
	}
	stats, err := s.repositoryAggregate(ctx, repo.Alias, repo.Name)
	if err != nil {
		return err
	}
	repo.PullCount = stats.PullCount
	repo.BlobBytes = stats.BlobBytes
	repo.BlobLinkCount = stats.BlobLinkCount
	repo.LastPullAt = stats.LastPullAt
	repo.LastBlobAccessAt = stats.LastBlobAccessAt
	repo.LastActivityAt = stats.LastActivityAt
	repo.UpdatedAt = metadataTimestamp(at)
	row := repositoryRecordToRow(*repo)
	if err := s.updateRepositoryRow(ctx, row); err != nil {
		return err
	}
	return s.refreshUpstreamMetadata(ctx, repo.UpstreamID, repo.UpdatedAt)
}

func (s *SQLiteStore) refreshRepositoriesForBlob(ctx context.Context, digest string, at time.Time) error {
	rows, err := repository.Query(s.repoBlobs).
		Where(sqliteRepoBlobRows.Digest.Eq(digest)).
		List(ctx)
	if err != nil {
		return wrapError(err, "list repository blob metadata for refresh")
	}
	var refreshErr error
	seen := map[string]struct{}{}
	rows.Range(func(_ int, row repoBlobRow) bool {
		key := repositoryMetadataKey(row.Alias, row.Repository)
		if _, ok := seen[key]; ok {
			return true
		}
		seen[key] = struct{}{}
		if err := s.refreshRepositoryMetadata(ctx, row.Alias, row.Repository, at); err != nil {
			refreshErr = err
			return false
		}
		return true
	})
	return refreshErr
}

func (s *SQLiteStore) refreshUpstreamMetadata(ctx context.Context, upstreamID int64, at time.Time) error {
	row, err := repository.By(s.upstreams, sqliteUpstreamRows.ID).Get(ctx, upstreamID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return wrapError(err, "get upstream metadata")
	}
	stats, err := s.upstreamAggregate(ctx, upstreamID)
	if err != nil {
		return err
	}
	record := upstreamRowToRecord(row)
	record.RepositoryCount = nullInt64(stats.RepositoryCount)
	record.PullCount = nullInt64(stats.PullCount)
	record.BlobBytes = nullInt64(stats.BlobBytes)
	record.BlobLinkCount = nullInt64(stats.BlobLinkCount)
	record.LastActivityAt = nullUnixNanoTime(stats.LastActivityAt)
	record.UpdatedAt = metadataTimestamp(at)
	return s.updateUpstreamRow(ctx, upstreamRecordToRow(*record))
}

func (s *SQLiteStore) updateRepositoryRow(ctx context.Context, row repositoryRow) error {
	_, err := repository.By(s.repositories, sqliteRepositoryRows.Key).Update(ctx, row.Key,
		sqliteRepositoryRows.UpstreamID.Set(row.UpstreamID),
		sqliteRepositoryRows.Alias.Set(row.Alias),
		sqliteRepositoryRows.Name.Set(row.Name),
		sqliteRepositoryRows.PullCount.Set(row.PullCount),
		sqliteRepositoryRows.BlobBytes.Set(row.BlobBytes),
		sqliteRepositoryRows.BlobLinkCount.Set(row.BlobLinkCount),
		sqliteRepositoryRows.LastPullAt.Set(row.LastPullAt),
		sqliteRepositoryRows.LastBlobAccessAt.Set(row.LastBlobAccessAt),
		sqliteRepositoryRows.LastActivityAt.Set(row.LastActivityAt),
		sqliteRepositoryRows.CreatedAt.Set(row.CreatedAt),
		sqliteRepositoryRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return wrapError(err, "update repository metadata")
	}
	return nil
}

func (s *SQLiteStore) updateUpstreamRow(ctx context.Context, row upstreamRow) error {
	_, err := repository.By(s.upstreams, sqliteUpstreamRows.Alias).Update(ctx, row.Alias,
		sqliteUpstreamRows.RepositoryCount.Set(row.RepositoryCount),
		sqliteUpstreamRows.PullCount.Set(row.PullCount),
		sqliteUpstreamRows.BlobBytes.Set(row.BlobBytes),
		sqliteUpstreamRows.BlobLinkCount.Set(row.BlobLinkCount),
		sqliteUpstreamRows.LastActivityAt.Set(row.LastActivityAt),
		sqliteUpstreamRows.CreatedAt.Set(row.CreatedAt),
		sqliteUpstreamRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return wrapError(err, "update upstream metadata")
	}
	return nil
}
