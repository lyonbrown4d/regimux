package meta

import (
	"context"
	"errors"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) Manifest(ctx context.Context, key ManifestKey) (*ManifestRecord, bool, error) {
	key, err := normalizeManifestKey(key)
	if err != nil {
		return nil, false, err
	}
	return s.manifestByKey(ctx, key.String())
}

func (s *SQLStore) UpsertManifest(ctx context.Context, record ManifestRecord) (*ManifestRecord, error) {
	key, record, err := normalizeManifestRecord(record)
	if err != nil {
		return nil, err
	}
	record = preserveManifestTimes(record, func() (*ManifestRecord, bool, error) {
		return s.Manifest(ctx, key)
	})
	row, err := s.mapper.ManifestRecordToRow(record)
	if err != nil {
		return nil, err
	}
	writeErr := s.writeManifestRow(ctx, key, &record, row)
	if writeErr != nil {
		return nil, writeErr
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, record.UpdatedAt); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLStore) DeleteManifest(ctx context.Context, key ManifestKey) error {
	key, err := normalizeManifestKey(key)
	if err != nil {
		return err
	}
	_, err = repository.By(s.manifest, sqlManifestRows.Key).Delete(ctx, key.String())
	if err != nil {
		return wrapError(err, "delete manifest metadata")
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, metadataNow()); err != nil {
		return err
	}
	return nil
}

func (s *SQLStore) GetManifest(ctx context.Context, key string) (*ManifestRecord, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, errorf("%w: manifest key is required", ErrInvalidKey)
	}
	return s.manifestByKey(ctx, key)
}

func (s *SQLStore) PutManifest(ctx context.Context, record ManifestRecord) error {
	if _, err := s.UpsertManifest(ctx, record); err != nil {
		return wrapError(err, "put manifest metadata")
	}
	return nil
}

func (s *SQLStore) ListManifests(ctx context.Context) (*collectionlist.List[ManifestRecord], error) {
	rows, err := s.manifest.List(ctx, nil)
	if err != nil {
		return nil, wrapError(err, "list manifest metadata")
	}
	return mapRows(rows, s.mapper.ManifestRowToRecord)
}

func (s *SQLStore) manifestByKey(ctx context.Context, key string) (*ManifestRecord, bool, error) {
	row, err := repository.By(s.manifest, sqlManifestRows.Key).Get(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get manifest metadata")
	}
	record, err := s.mapper.ManifestRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLStore) writeManifestRow(ctx context.Context, key ManifestKey, record *ManifestRecord, row manifestRow) error {
	if record.ID != 0 {
		return s.updateManifestRow(ctx, row)
	}
	if err := s.manifest.Create(ctx, &row); err != nil {
		recovered, recoverErr := s.updateManifestAfterCreateRace(ctx, key, record)
		if recoverErr != nil {
			return recoverErr
		}
		if recovered {
			return nil
		}
		return wrapError(err, "upsert manifest metadata")
	}
	record.ID = row.ID
	return nil
}

func (s *SQLStore) updateManifestAfterCreateRace(ctx context.Context, key ManifestKey, record *ManifestRecord) (bool, error) {
	current, ok, err := s.Manifest(ctx, key)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	record.ID = current.ID
	record.CreatedAt = current.CreatedAt
	row, err := s.mapper.ManifestRecordToRow(*record)
	if err != nil {
		return false, err
	}
	return true, s.updateManifestRow(ctx, row)
}

func (s *SQLStore) updateManifestRow(ctx context.Context, row manifestRow) error {
	return patchRowByKey(ctx, s.manifest, sqlManifestRows.Key, row.Key, "upsert manifest metadata",
		sqlManifestRows.Alias.Set(row.Alias),
		sqlManifestRows.Repository.Set(row.Repository),
		sqlManifestRows.Reference.Set(row.Reference),
		sqlManifestRows.AcceptKey.Set(row.AcceptKey),
		sqlManifestRows.Digest.Set(row.Digest),
		sqlManifestRows.MediaType.Set(row.MediaType),
		sqlManifestRows.Size.Set(row.Size),
		sqlManifestRows.ObjectKey.Set(row.ObjectKey),
		sqlManifestRows.Headers.Set(row.Headers),
		sqlManifestRows.ExpiresAt.Set(row.ExpiresAt),
		sqlManifestRows.CreatedAt.Set(row.CreatedAt),
		sqlManifestRows.UpdatedAt.Set(row.UpdatedAt),
	)
}

func preserveManifestTimes(record ManifestRecord, existing func() (*ManifestRecord, bool, error)) ManifestRecord {
	now := metadataNow()
	if existing != nil {
		current, ok, err := existing()
		if err == nil && ok {
			record.ID = current.ID
			record.CreatedAt = current.CreatedAt
		}
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	return record
}
