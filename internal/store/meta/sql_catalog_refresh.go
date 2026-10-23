package meta

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/querydsl"
	"github.com/arcgolabs/dbx/repository"
	"github.com/samber/lo"
)

func (s *SQLStore) ensureUpstreamMetadata(ctx context.Context, alias string, at time.Time) (*Upstream, error) {
	alias, err := normalizeUpstreamAlias(alias)
	if err != nil {
		return nil, err
	}
	row, err := s.upstreams.By(sqlUpstreamRows.Alias).Get(ctx, alias)
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
	recovered, ok, err := s.createUpstreamMetadataRow(ctx, alias, &row)
	if err != nil {
		return nil, err
	}
	if ok {
		return recovered, nil
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLStore) createUpstreamMetadataRow(ctx context.Context, alias string, row *upstreamRow) (*Upstream, bool, error) {
	if err := s.upstreams.Create(ctx, row); err != nil {
		recovered, ok, recoverErr := s.upstreamMetadataAfterCreateRace(ctx, alias)
		if recoverErr != nil {
			return nil, false, recoverErr
		}
		if ok {
			return recovered, true, nil
		}
		return nil, false, wrapError(err, "create upstream metadata")
	}
	return nil, false, nil
}

func (s *SQLStore) upstreamMetadataAfterCreateRace(ctx context.Context, alias string) (*Upstream, bool, error) {
	row, err := s.upstreams.By(sqlUpstreamRows.Alias).Get(ctx, alias)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get upstream metadata after create race")
	}
	record, err := s.mapper.UpstreamRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLStore) acquireCatalogRefresh(ctx context.Context) (func(), error) {
	if s.driver != metaDriverSQLite {
		return func() {}, nil
	}
	if err := s.catalogMu.Acquire(ctx, 1); err != nil {
		return nil, wrapError(err, "wait for catalog refresh")
	}
	return func() { s.catalogMu.Release(1) }, nil
}

func (s *SQLStore) refreshRepositoryMetadataBatchOnce(ctx context.Context, alias string, updates map[string]time.Time) error {
	if len(updates) == 0 {
		return nil
	}

	// Only the upstream must exist before taking its transaction lock. Repository
	// rows are inserted in batches under that lock without per-row commits.
	var latestAt time.Time
	for _, at := range updates {
		latestAt = maxTime(latestAt, at)
	}
	if _, err := s.ensureUpstreamMetadata(ctx, alias, latestAt); err != nil {
		return err
	}
	return s.withCatalogRefreshTx(ctx, alias, updates)
}

func (s *SQLStore) refreshRepositoriesForBlob(ctx context.Context, digest string, at time.Time) error {
	key := catalogRefreshKey{Digest: digest}
	return s.refreshCatalogMetadata(ctx, key, at, func(sharedCtx context.Context, latestAt time.Time) error {
		return s.refreshRepositoriesForBlobOnce(sharedCtx, digest, latestAt)
	})
}

func (s *SQLStore) refreshRepositoriesForBlobOnce(ctx context.Context, digest string, at time.Time) error {
	rows, err := repository.Query(s.repoBlobs).
		Where(sqlRepoBlobRows.Digest.Eq(digest)).
		List(ctx)
	if err != nil {
		return wrapError(err, "list repository blob metadata for refresh")
	}
	uniqueRows := lo.UniqBy(rows.Values(), func(row repoBlobRow) string {
		return repositoryMetadataKey(row.Alias, row.Repository)
	})
	updatesByAlias := make(map[string]map[string]time.Time)
	for i := range uniqueRows {
		row := uniqueRows[i]
		updates := updatesByAlias[row.Alias]
		if updates == nil {
			updates = make(map[string]time.Time)
			updatesByAlias[row.Alias] = updates
		}
		updates[row.Repository] = metadataTimestamp(at)
	}
	aliases := make([]string, 0, len(updatesByAlias))
	for alias := range updatesByAlias {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		if err := s.refreshRepositoryMetadataBatch(ctx, alias, updatesByAlias[alias]); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) refreshUpstreamMetadata(ctx context.Context, session dbx.Session, upstreamID int64, row upstreamRow, at time.Time) error {
	stats, err := s.upstreamAggregate(ctx, session, upstreamID)
	if err != nil {
		return err
	}
	record, err := s.mapper.UpstreamRowToRecord(row)
	if err != nil {
		return err
	}
	record.RepositoryCount = nullInt64(stats.RepositoryCount)
	record.PullCount = nullInt64(stats.PullCount)
	record.PolicyDeniedPullCount = nullInt64(stats.PolicyDeniedPullCount)
	record.BlobBytes = nullInt64(stats.BlobBytes)
	record.BlobLinkCount = nullInt64(stats.BlobLinkCount)
	record.LastPolicyDeniedAt = nullUnixNanoTime(stats.LastPolicyDeniedAt)
	record.LastActivityAt = nullUnixNanoTime(stats.LastActivityAt)
	record.UpdatedAt = maxTime(metadataTimestamp(at), record.UpdatedAt, record.LastActivityAt)
	updateRow, err := s.mapper.UpstreamRecordToRow(*record)
	if err != nil {
		return err
	}
	return s.updateUpstreamRow(ctx, session, updateRow)
}

func (s *SQLStore) updateRepositoryRow(ctx context.Context, session dbx.Session, row repositoryRow) error {
	_, err := dbx.Exec(ctx, session, querydsl.Update(sqlRepositoryRows).Set(
		sqlRepositoryRows.UpstreamID.Set(row.UpstreamID),
		sqlRepositoryRows.Alias.Set(row.Alias),
		sqlRepositoryRows.Name.Set(row.Name),
		sqlRepositoryRows.PullCount.Set(row.PullCount),
		sqlRepositoryRows.PolicyDeniedPullCount.Set(row.PolicyDeniedPullCount),
		sqlRepositoryRows.BlobBytes.Set(row.BlobBytes),
		sqlRepositoryRows.BlobLinkCount.Set(row.BlobLinkCount),
		sqlRepositoryRows.LastPullAt.Set(row.LastPullAt),
		sqlRepositoryRows.LastPolicyDeniedAt.Set(row.LastPolicyDeniedAt),
		sqlRepositoryRows.LastBlobAccessAt.Set(row.LastBlobAccessAt),
		sqlRepositoryRows.LastActivityAt.Set(row.LastActivityAt),
		sqlRepositoryRows.CreatedAt.Set(row.CreatedAt),
		sqlRepositoryRows.UpdatedAt.Set(row.UpdatedAt),
	).Where(sqlRepositoryRows.Key.Eq(row.Key)))
	if err != nil {
		return wrapError(err, "update repository metadata")
	}
	return nil
}

func (s *SQLStore) updateUpstreamRow(ctx context.Context, session dbx.Session, row upstreamRow) error {
	_, err := dbx.Exec(ctx, session, querydsl.Update(sqlUpstreamRows).Set(
		sqlUpstreamRows.RepositoryCount.Set(row.RepositoryCount),
		sqlUpstreamRows.PullCount.Set(row.PullCount),
		sqlUpstreamRows.PolicyDeniedPullCount.Set(row.PolicyDeniedPullCount),
		sqlUpstreamRows.BlobBytes.Set(row.BlobBytes),
		sqlUpstreamRows.BlobLinkCount.Set(row.BlobLinkCount),
		sqlUpstreamRows.LastPolicyDeniedAt.Set(row.LastPolicyDeniedAt),
		sqlUpstreamRows.LastActivityAt.Set(row.LastActivityAt),
		sqlUpstreamRows.CreatedAt.Set(row.CreatedAt),
		sqlUpstreamRows.UpdatedAt.Set(row.UpdatedAt),
	).Where(sqlUpstreamRows.Alias.Eq(row.Alias)))
	if err != nil {
		return wrapError(err, "update upstream metadata")
	}
	return nil
}
