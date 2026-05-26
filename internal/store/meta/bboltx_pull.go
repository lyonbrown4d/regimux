package meta

import (
	"context"
	"time"
)

func (s *BboltStore) Pull(ctx context.Context, key PullKey) (*PullRecord, bool, error) {
	key, err := normalizePullKey(key)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := s.pulls.Get(ctx, key)
	if err != nil {
		return nil, false, wrapError(err, "get pull metadata")
	}
	if !ok {
		return nil, false, nil
	}
	return &record, true, nil
}

func (s *BboltStore) RecordPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error) {
	return s.updatePull(ctx, key, at, func(record *PullRecord, now time.Time) {
		record.Count++
		record.LastPullAt = now
	})
}

func (s *BboltStore) RecordUpstreamPull(ctx context.Context, key PullKey, at time.Time) (*PullRecord, error) {
	return s.updatePull(ctx, key, at, func(record *PullRecord, now time.Time) {
		record.LastUpstreamPullAt = now
	})
}

func (s *BboltStore) updatePull(ctx context.Context, key PullKey, at time.Time, update func(*PullRecord, time.Time)) (*PullRecord, error) {
	key, err := normalizePullKey(key)
	if err != nil {
		return nil, err
	}
	now := at.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	record, ok, err := s.Pull(ctx, key)
	if err != nil {
		return nil, err
	}
	if !ok {
		record = &PullRecord{
			Alias:      key.Alias,
			Repository: key.Repository,
			Reference:  key.Reference,
			CreatedAt:  now,
		}
	}
	if update != nil {
		update(record, now)
	}
	record.UpdatedAt = now

	normalizedKey, normalized, err := normalizePullRecord(*record)
	if err != nil {
		return nil, err
	}
	if err := s.pulls.Put(ctx, normalizedKey, normalized); err != nil {
		return nil, wrapError(err, "put pull metadata")
	}
	return &normalized, nil
}

func (s *BboltStore) ListPulls(ctx context.Context) ([]PullRecord, error) {
	entries, err := s.pulls.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list pull metadata")
	}
	records := make([]PullRecord, 0, len(entries))
	for _, entry := range entries {
		records = append(records, entry.Value)
	}
	return records, nil
}
