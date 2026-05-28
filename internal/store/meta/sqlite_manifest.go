package meta

import (
	"context"
	"errors"
	"strings"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLiteStore) Manifest(ctx context.Context, key ManifestKey) (*ManifestRecord, bool, error) {
	key, err := normalizeManifestKey(key)
	if err != nil {
		return nil, false, err
	}
	return s.manifestByKey(ctx, key.String())
}

func (s *SQLiteStore) UpsertManifest(ctx context.Context, record ManifestRecord) (*ManifestRecord, error) {
	key, record, err := normalizeManifestRecord(record)
	if err != nil {
		return nil, err
	}
	record = preserveManifestTimes(record, func() (*ManifestRecord, bool, error) {
		return s.Manifest(ctx, key)
	})
	row, err := manifestRecordToRow(record)
	if err != nil {
		return nil, err
	}
	if record.ID != 0 {
		if err := s.updateManifestRow(ctx, row); err != nil {
			return nil, err
		}
		return &record, nil
	}
	if err := s.manifest.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "upsert manifest metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLiteStore) DeleteManifest(ctx context.Context, key ManifestKey) error {
	key, err := normalizeManifestKey(key)
	if err != nil {
		return err
	}
	_, err = repository.By(s.manifest, sqliteManifestRows.Key).Delete(ctx, key.String())
	if err != nil {
		return wrapError(err, "delete manifest metadata")
	}
	return nil
}

func (s *SQLiteStore) GetManifest(ctx context.Context, key string) (*ManifestRecord, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, errorf("%w: manifest key is required", ErrInvalidKey)
	}
	return s.manifestByKey(ctx, key)
}

func (s *SQLiteStore) PutManifest(ctx context.Context, record ManifestRecord) error {
	if _, err := s.UpsertManifest(ctx, record); err != nil {
		return wrapError(err, "put manifest metadata")
	}
	return nil
}

func (s *SQLiteStore) ListManifests(ctx context.Context) ([]ManifestRecord, error) {
	rows, err := s.manifest.List(ctx, nil)
	if err != nil {
		return nil, wrapError(err, "list manifest metadata")
	}
	records := make([]ManifestRecord, 0, rows.Len())
	var decodeErr error
	rows.Range(func(_ int, row manifestRow) bool {
		record, err := manifestRowToRecord(row)
		if err != nil {
			decodeErr = err
			return false
		}
		records = append(records, *record)
		return true
	})
	if decodeErr != nil {
		return nil, decodeErr
	}
	return records, nil
}

func (s *SQLiteStore) manifestByKey(ctx context.Context, key string) (*ManifestRecord, bool, error) {
	row, err := repository.By(s.manifest, sqliteManifestRows.Key).Get(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get manifest metadata")
	}
	record, err := manifestRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLiteStore) updateManifestRow(ctx context.Context, row manifestRow) error {
	_, err := repository.By(s.manifest, sqliteManifestRows.Key).Update(ctx, row.Key,
		sqliteManifestRows.Alias.Set(row.Alias),
		sqliteManifestRows.Repository.Set(row.Repository),
		sqliteManifestRows.Reference.Set(row.Reference),
		sqliteManifestRows.AcceptKey.Set(row.AcceptKey),
		sqliteManifestRows.Digest.Set(row.Digest),
		sqliteManifestRows.MediaType.Set(row.MediaType),
		sqliteManifestRows.Size.Set(row.Size),
		sqliteManifestRows.ObjectKey.Set(row.ObjectKey),
		sqliteManifestRows.Headers.Set(row.Headers),
		sqliteManifestRows.ExpiresAt.Set(row.ExpiresAt),
		sqliteManifestRows.CreatedAt.Set(row.CreatedAt),
		sqliteManifestRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return wrapError(err, "upsert manifest metadata")
	}
	return nil
}

func preserveManifestTimes(record ManifestRecord, existing func() (*ManifestRecord, bool, error)) ManifestRecord {
	now := sqliteNow()
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
