package meta

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/arcgolabs/dbx/querydsl"
)

func (s *SQLStore) DeleteBlobIfUnchanged(ctx context.Context, expected BlobRecord) (bool, error) {
	key, err := normalizeBlobKey(BlobKey{Digest: expected.Digest})
	if err != nil {
		return false, err
	}
	query := querydsl.DeleteFrom(s.blobs.Schema()).Where(querydsl.And(
		sqlBlobRows.Digest.Eq(key.Digest),
		sqlBlobRows.Size.Eq(expected.Size),
		sqlBlobRows.ObjectKey.Eq(expected.ObjectKey),
		sqlBlobRows.LastAccessAt.Eq(unixNano(expected.LastAccessAt)),
		sqlBlobRows.UpdatedAt.Eq(unixNano(expected.UpdatedAt)),
	))
	deleted, err := deleteIfMatched(ctx, s.blobs, query, "delete blob metadata conditionally")
	if err != nil || !deleted {
		return deleted, err
	}
	if err := s.refreshRepositoriesForBlob(ctx, key.Digest, metadataNow()); err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLStore) DeleteTagIfUnchanged(ctx context.Context, expected TagRecord) (bool, error) {
	key, err := normalizeTagKey(TagKey{Alias: expected.Alias, Repository: expected.Repository, Reference: expected.Reference})
	if err != nil {
		return false, err
	}
	query := querydsl.DeleteFrom(s.tags.Schema()).Where(querydsl.And(
		sqlTagRows.Key.Eq(key.String()), sqlTagRows.Digest.Eq(expected.Digest),
		sqlTagRows.UpdatedAt.Eq(unixNano(expected.UpdatedAt)), sqlTagRows.ExpiresAt.Eq(unixNano(expected.ExpiresAt)),
	))
	deleted, err := deleteIfMatched(ctx, s.tags, query, "delete tag metadata conditionally")
	if err != nil || !deleted {
		return deleted, err
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, metadataNow()); err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLStore) DeleteManifestIfUnchanged(ctx context.Context, expected ManifestRecord) (bool, error) {
	key, err := normalizeManifestKey(ManifestKey{Alias: expected.Alias, Repository: expected.Repository, Digest: expected.Digest})
	if err != nil {
		return false, err
	}
	query := querydsl.DeleteFrom(s.manifest.Schema()).Where(querydsl.And(
		sqlManifestRows.Key.Eq(key.String()), sqlManifestRows.Digest.Eq(expected.Digest),
		sqlManifestRows.ObjectKey.Eq(expected.ObjectKey), sqlManifestRows.UpdatedAt.Eq(unixNano(expected.UpdatedAt)),
	))
	deleted, err := deleteIfMatched(ctx, s.manifest, query, "delete manifest metadata conditionally")
	if err != nil || !deleted {
		return deleted, err
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, metadataNow()); err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLStore) DeleteRepoBlobIfUnchanged(ctx context.Context, expected RepoBlobRecord) (bool, error) {
	key, err := normalizeRepoBlobKey(RepoBlobKey{Alias: expected.Alias, Repository: expected.Repository, Digest: expected.Digest})
	if err != nil {
		return false, err
	}
	query := querydsl.DeleteFrom(s.repoBlobs.Schema()).Where(querydsl.And(
		sqlRepoBlobRows.Key.Eq(key.String()), sqlRepoBlobRows.Digest.Eq(expected.Digest),
		sqlRepoBlobRows.UpdatedAt.Eq(unixNano(expected.UpdatedAt)),
		sqlRepoBlobRows.LastAccessAt.Eq(unixNano(expected.LastAccessAt)),
		sqlRepoBlobRows.LastVerifiedAt.Eq(unixNano(expected.LastVerifiedAt)),
	))
	deleted, err := deleteIfMatched(ctx, s.repoBlobs, query, "delete repository blob metadata conditionally")
	if err != nil || !deleted {
		return deleted, err
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, metadataNow()); err != nil {
		return false, err
	}
	return true, nil
}

type conditionalDeleteRepository interface {
	Delete(context.Context, *querydsl.DeleteQuery) (sql.Result, error)
}

func deleteIfMatched(ctx context.Context, repository conditionalDeleteRepository, query *querydsl.DeleteQuery, operation string) (bool, error) {
	result, err := repository.Delete(ctx, query)
	if err != nil {
		return false, fmt.Errorf("%s: %w", operation, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count %s rows: %w", operation, err)
	}
	return rows != 0, nil
}
