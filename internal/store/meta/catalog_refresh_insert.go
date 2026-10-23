package meta

import (
	"context"
	"slices"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/querydsl"
)

// Keep inserts below even the older SQLite 999-parameter limit (15 columns).
const catalogRepositoryInsertBatchSize = 32

func (s *SQLStore) ensureCatalogRepositoryRows(
	ctx context.Context, tx *dbx.Tx, upstream upstreamRow, names []string, updates map[string]time.Time,
) error {
	for batch := range slices.Chunk(names, catalogRepositoryInsertBatchSize) {
		query, err := s.catalogRepositoryInsert(ctx, tx, upstream, batch, updates)
		if err != nil {
			return err
		}
		if _, err := dbx.Exec(ctx, tx, query); err != nil {
			return wrapError(err, "create repository metadata batch")
		}
	}
	return nil
}

func (s *SQLStore) catalogRepositoryInsert(
	ctx context.Context, tx *dbx.Tx, upstream upstreamRow, names []string, updates map[string]time.Time,
) (*querydsl.InsertQuery, error) {
	query := querydsl.InsertInto(sqlRepositoryRows)
	conflict := query.OnConflict(sqlRepositoryRows.Key)
	if s.driver == metaDriverMySQL {
		// MySQL DO NOTHING renders INSERT IGNORE, which also hides data errors.
		conflict.DoUpdateSet(sqlRepositoryRows.Key.SetColumn(sqlRepositoryRows.Key))
	} else {
		conflict.DoNothing()
	}
	for _, name := range names {
		at := metadataTimestamp(updates[name])
		row, err := s.mapper.RepositoryRecordToRow(Repository{
			UpstreamID: upstream.ID,
			Alias:      upstream.Alias,
			Name:       name,
			CreatedAt:  at,
			UpdatedAt:  at,
		})
		if err != nil {
			return nil, err
		}
		assignments, err := s.repositories.Mapper().InsertAssignmentsWithID(ctx, sqlRepositoryRows, &row, tx.IDGenerator())
		if err != nil {
			return nil, wrapError(err, "map repository metadata batch")
		}
		query.ValuesList(assignments)
	}
	return query, nil
}
