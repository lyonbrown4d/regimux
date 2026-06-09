package meta

import (
	"context"
	"database/sql"
	"time"

	"github.com/arcgolabs/dbx/querydsl"
	"github.com/arcgolabs/dbx/repository"
)

func metadataTimestamp(at time.Time) time.Time {
	at = at.UTC()
	if at.IsZero() {
		return metadataNow()
	}
	return at
}

func nullInt64(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func maxTime(values ...time.Time) time.Time {
	var out time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if out.IsZero() || value.After(out) {
			out = value
		}
	}
	return out
}

func patchRowByKey[E any, S repository.EntitySchema[E], T any](
	ctx context.Context,
	base *repository.Base[E, S],
	column repository.KeyColumn[T],
	value T,
	operation string,
	assignments ...querydsl.Assignment,
) error {
	_, err := repository.PatchSet(base, repository.KeySet(repository.Part(column, value))).
		Set(assignments...).
		Apply(ctx)
	if err != nil {
		return wrapError(err, "%s", operation)
	}
	return nil
}
