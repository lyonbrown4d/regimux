package meta

import (
	"database/sql"
	"time"
)

func metadataTimestamp(at time.Time) time.Time {
	at = at.UTC()
	if at.IsZero() {
		return sqliteNow()
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
