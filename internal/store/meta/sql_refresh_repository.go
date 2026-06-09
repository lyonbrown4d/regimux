package meta

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) RefreshIntent(ctx context.Context, key RefreshIntentKey) (*RefreshIntentRecord, bool, error) {
	key, err := normalizeRefreshIntentKey(key)
	if err != nil {
		return nil, false, err
	}
	row, err := repository.By(s.refreshIntents, sqlRefreshIntentRows.Key).Get(ctx, key.String())
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get refresh intent metadata")
	}
	record, err := s.mapper.RefreshIntentRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLStore) QueueRefreshIntent(ctx context.Context, record RefreshIntentRecord, at time.Time, window time.Duration) (*RefreshIntentRecord, bool, error) {
	key, record, err := normalizeRefreshIntentRecord(record)
	if err != nil {
		return nil, false, err
	}
	if at.IsZero() {
		at = metadataNow()
	}
	now := at.UTC()
	if window <= 0 {
		window = 10 * time.Minute
	}

	if existing, ok, err := s.RefreshIntent(ctx, key); err != nil {
		return nil, false, err
	} else if ok {
		existing.LastSeenAt = now
		existing.Skipped++
		existing.UpdatedAt = now
		if err := s.updateRefreshIntentRow(ctx, *existing); err != nil {
			return nil, false, err
		}
		return existing, false, nil
	}

	record.DueAt = now.Add(window)
	record.LastSeenAt = now
	record.CreatedAt = now
	record.UpdatedAt = now
	row, err := s.mapper.RefreshIntentRecordToRow(record)
	if err != nil {
		return nil, false, err
	}
	if err := s.refreshIntents.Create(ctx, &row); err != nil {
		if existing, ok, lookupErr := s.RefreshIntent(ctx, key); lookupErr == nil && ok {
			existing.LastSeenAt = now
			existing.Skipped++
			existing.UpdatedAt = now
			if updateErr := s.updateRefreshIntentRow(ctx, *existing); updateErr != nil {
				return nil, false, updateErr
			}
			return existing, false, nil
		}
		return nil, false, wrapError(err, "queue refresh intent metadata")
	}
	record.ID = row.ID
	return &record, true, nil
}

func (s *SQLStore) ConsumeDueRefreshIntents(ctx context.Context, at time.Time, limit int) ([]RefreshIntentRecord, error) {
	if at.IsZero() {
		at = metadataNow()
	}
	query := repository.Query(s.refreshIntents).
		Where(sqlRefreshIntentRows.DueAt.Le(unixNano(at))).
		OrderBy(sqlRefreshIntentRows.DueAt.Asc(), sqlRefreshIntentRows.ID.Asc())
	if limit > 0 {
		query = query.Limit(limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list due refresh intents metadata")
	}

	records := make([]RefreshIntentRecord, 0, rows.Len())
	rows.Range(func(_ int, row refreshIntentRow) bool {
		record, mapErr := s.mapper.RefreshIntentRowToRecord(row)
		if mapErr != nil {
			err = mapErr
			return false
		}
		claimed, claimErr := s.deleteRefreshIntentByKey(ctx, record.Key)
		if claimErr != nil {
			err = claimErr
			return false
		}
		if claimed {
			records = append(records, *record)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (s *SQLStore) updateRefreshIntentRow(ctx context.Context, record RefreshIntentRecord) error {
	row, err := s.mapper.RefreshIntentRecordToRow(record)
	if err != nil {
		return err
	}
	_, err = repository.By(s.refreshIntents, sqlRefreshIntentRows.Key).Update(ctx, row.Key,
		sqlRefreshIntentRows.Ecosystem.Set(row.Ecosystem),
		sqlRefreshIntentRows.Kind.Set(row.Kind),
		sqlRefreshIntentRows.Alias.Set(row.Alias),
		sqlRefreshIntentRows.Repository.Set(row.Repository),
		sqlRefreshIntentRows.Reference.Set(row.Reference),
		sqlRefreshIntentRows.Accept.Set(row.Accept),
		sqlRefreshIntentRows.DueAt.Set(row.DueAt),
		sqlRefreshIntentRows.LastSeenAt.Set(row.LastSeenAt),
		sqlRefreshIntentRows.Skipped.Set(row.Skipped),
		sqlRefreshIntentRows.CreatedAt.Set(row.CreatedAt),
		sqlRefreshIntentRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return wrapError(err, "update refresh intent metadata")
	}
	return nil
}

func (s *SQLStore) deleteRefreshIntentByKey(ctx context.Context, key string) (bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return false, nil
	}
	_, err := repository.By(s.refreshIntents, sqlRefreshIntentRows.Key).Delete(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, wrapError(err, "claim refresh intent metadata")
	}
	return true, nil
}

func normalizeRefreshIntentRecord(record RefreshIntentRecord) (RefreshIntentKey, RefreshIntentRecord, error) {
	key, err := normalizeRefreshIntentKey(RefreshIntentKey{
		Ecosystem:  record.Ecosystem,
		Kind:       record.Kind,
		Alias:      record.Alias,
		Repository: record.Repository,
		Reference:  record.Reference,
		Accept:     record.Accept,
	})
	if err != nil {
		return RefreshIntentKey{}, RefreshIntentRecord{}, err
	}
	record.Key = key.String()
	record.Ecosystem = key.Ecosystem
	record.Kind = key.Kind
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Reference = key.Reference
	record.Accept = key.Accept
	if record.Skipped < 0 {
		record.Skipped = 0
	}
	return key, record, nil
}

func normalizeRefreshIntentKey(key RefreshIntentKey) (RefreshIntentKey, error) {
	ecosystemName, err := required("ecosystem", string(key.Ecosystem))
	if err != nil {
		return RefreshIntentKey{}, err
	}
	alias, err := required("alias", key.Alias)
	if err != nil {
		return RefreshIntentKey{}, err
	}
	repository, err := required("repository", key.Repository)
	if err != nil {
		return RefreshIntentKey{}, err
	}
	reference, err := required("reference", key.Reference)
	if err != nil {
		return RefreshIntentKey{}, err
	}
	return RefreshIntentKey{
		Ecosystem:  RefreshIntentEcosystem(ecosystemName),
		Kind:       RefreshIntentKind(strings.TrimSpace(string(key.Kind))),
		Alias:      alias,
		Repository: repository,
		Reference:  reference,
		Accept:     strings.TrimSpace(key.Accept),
	}, nil
}
