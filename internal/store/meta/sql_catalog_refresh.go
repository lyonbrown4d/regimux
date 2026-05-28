package meta

import (
	"context"
	"errors"
	"time"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) ensureRepositoryMetadata(ctx context.Context, alias, name string, at time.Time) (*Repository, error) {
	upstream, err := s.ensureUpstreamMetadata(ctx, alias, at)
	if err != nil {
		return nil, err
	}
	name, err = normalizeRepositoryName(name)
	if err != nil {
		return nil, err
	}
	key := repositoryMetadataKey(upstream.Alias, name)
	row, err := repository.By(s.repositories, sqlRepositoryRows.Key).Get(ctx, key)
	if err == nil {
		record, mapErr := s.mapper.RepositoryRowToRecord(row)
		if mapErr != nil {
			return nil, mapErr
		}
		return record, nil
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
	row, err = s.mapper.RepositoryRecordToRow(record)
	if err != nil {
		return nil, err
	}
	if err := s.repositories.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "create repository metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLStore) ensureUpstreamMetadata(ctx context.Context, alias string, at time.Time) (*Upstream, error) {
	alias, err := normalizeUpstreamAlias(alias)
	if err != nil {
		return nil, err
	}
	row, err := repository.By(s.upstreams, sqlUpstreamRows.Alias).Get(ctx, alias)
	if err == nil {
		record, mapErr := s.mapper.UpstreamRowToRecord(row)
		if mapErr != nil {
			return nil, mapErr
		}
		return record, nil
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
	row, err = s.mapper.UpstreamRecordToRow(record)
	if err != nil {
		return nil, err
	}
	if err := s.upstreams.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "create upstream metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLStore) refreshRepositoryMetadata(ctx context.Context, alias, name string, at time.Time) error {
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
	row, err := s.mapper.RepositoryRecordToRow(*repo)
	if err != nil {
		return err
	}
	if err := s.updateRepositoryRow(ctx, row); err != nil {
		return err
	}
	return s.refreshUpstreamMetadata(ctx, repo.UpstreamID, repo.UpdatedAt)
}

func (s *SQLStore) refreshRepositoriesForBlob(ctx context.Context, digest string, at time.Time) error {
	rows, err := repository.Query(s.repoBlobs).
		Where(sqlRepoBlobRows.Digest.Eq(digest)).
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

func (s *SQLStore) refreshUpstreamMetadata(ctx context.Context, upstreamID int64, at time.Time) error {
	row, err := repository.By(s.upstreams, sqlUpstreamRows.ID).Get(ctx, upstreamID)
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
	record, err := s.mapper.UpstreamRowToRecord(row)
	if err != nil {
		return err
	}
	record.RepositoryCount = nullInt64(stats.RepositoryCount)
	record.PullCount = nullInt64(stats.PullCount)
	record.BlobBytes = nullInt64(stats.BlobBytes)
	record.BlobLinkCount = nullInt64(stats.BlobLinkCount)
	record.LastActivityAt = nullUnixNanoTime(stats.LastActivityAt)
	record.UpdatedAt = metadataTimestamp(at)
	updateRow, err := s.mapper.UpstreamRecordToRow(*record)
	if err != nil {
		return err
	}
	return s.updateUpstreamRow(ctx, updateRow)
}

func (s *SQLStore) updateRepositoryRow(ctx context.Context, row repositoryRow) error {
	_, err := repository.By(s.repositories, sqlRepositoryRows.Key).Update(ctx, row.Key,
		sqlRepositoryRows.UpstreamID.Set(row.UpstreamID),
		sqlRepositoryRows.Alias.Set(row.Alias),
		sqlRepositoryRows.Name.Set(row.Name),
		sqlRepositoryRows.PullCount.Set(row.PullCount),
		sqlRepositoryRows.BlobBytes.Set(row.BlobBytes),
		sqlRepositoryRows.BlobLinkCount.Set(row.BlobLinkCount),
		sqlRepositoryRows.LastPullAt.Set(row.LastPullAt),
		sqlRepositoryRows.LastBlobAccessAt.Set(row.LastBlobAccessAt),
		sqlRepositoryRows.LastActivityAt.Set(row.LastActivityAt),
		sqlRepositoryRows.CreatedAt.Set(row.CreatedAt),
		sqlRepositoryRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return wrapError(err, "update repository metadata")
	}
	return nil
}

func (s *SQLStore) updateUpstreamRow(ctx context.Context, row upstreamRow) error {
	_, err := repository.By(s.upstreams, sqlUpstreamRows.Alias).Update(ctx, row.Alias,
		sqlUpstreamRows.RepositoryCount.Set(row.RepositoryCount),
		sqlUpstreamRows.PullCount.Set(row.PullCount),
		sqlUpstreamRows.BlobBytes.Set(row.BlobBytes),
		sqlUpstreamRows.BlobLinkCount.Set(row.BlobLinkCount),
		sqlUpstreamRows.LastActivityAt.Set(row.LastActivityAt),
		sqlUpstreamRows.CreatedAt.Set(row.CreatedAt),
		sqlUpstreamRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return wrapError(err, "update upstream metadata")
	}
	return nil
}
