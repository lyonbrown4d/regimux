package meta

import (
	"context"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dbx/querydsl"
	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) RequestPrefetchControl(ctx context.Context, record PrefetchControlRecord) (*PrefetchControlRecord, error) {
	record.Action = strings.ToLower(strings.TrimSpace(record.Action))
	if record.Action == "" {
		return nil, errorf("%w: prefetch control action is required", ErrInvalidValue)
	}
	now := metadataNow()
	if record.RequestedAt.IsZero() {
		record.RequestedAt = now
	}
	record.CreatedAt = now
	record.UpdatedAt = now
	row, err := s.mapper.PrefetchControlRecordToRow(record)
	if err != nil {
		return nil, err
	}
	if err := s.prefetchControls.Create(ctx, &row); err != nil {
		return nil, wrapError(err, "create prefetch control metadata")
	}
	record.ID = row.ID
	return &record, nil
}

func (s *SQLStore) ConsumePrefetchControl(ctx context.Context, action string, at time.Time) (*PrefetchControlRecord, bool, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return nil, false, errorf("%w: prefetch control action is required", ErrInvalidValue)
	}
	if at.IsZero() {
		at = metadataNow()
	}
	rows, err := repository.Query(s.prefetchControls).
		Where(querydsl.And(
			sqlPrefetchControlRows.Action.Eq(action),
			sqlPrefetchControlRows.ConsumedAt.Eq(0),
		)).
		OrderBy(sqlPrefetchControlRows.RequestedAt.Asc(), sqlPrefetchControlRows.ID.Asc()).
		Limit(1).
		List(ctx)
	if err != nil {
		return nil, false, wrapError(err, "find prefetch control metadata")
	}
	row, ok := rows.GetFirstOption().Get()
	if !ok {
		return nil, false, nil
	}
	record, err := s.mapper.PrefetchControlRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	record.ConsumedAt = at.UTC()
	record.UpdatedAt = metadataNow()
	row, err = s.mapper.PrefetchControlRecordToRow(*record)
	if err != nil {
		return nil, false, err
	}
	err = patchRowByKey(ctx, s.prefetchControls, sqlPrefetchControlRows.ID, row.ID, "consume prefetch control metadata",
		sqlPrefetchControlRows.ConsumedAt.Set(row.ConsumedAt),
		sqlPrefetchControlRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLStore) ListPrefetchControls(ctx context.Context, opts ...PrefetchControlListOption) (*collectionlist.List[PrefetchControlRecord], error) {
	options := prefetchControlListOptions(opts...)
	query := repository.Query(s.prefetchControls)
	if options.RecentFirst {
		query = query.OrderBy(
			sqlPrefetchControlRows.RequestedAt.Desc(),
			sqlPrefetchControlRows.CreatedAt.Desc(),
			sqlPrefetchControlRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list prefetch control metadata")
	}
	return s.prefetchControlRowsToRecords(rows)
}

func (s *SQLStore) prefetchControlRowsToRecords(rows rowCollection[prefetchControlRow]) (*collectionlist.List[PrefetchControlRecord], error) {
	return mapRows(rows, s.mapper.PrefetchControlRowToRecord)
}
