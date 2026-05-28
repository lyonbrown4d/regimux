package meta

import (
	"context"
	"database/sql"
	"strings"

	sqlitedialect "github.com/arcgolabs/dbx/dialect/sqlite"
	metasqlite "github.com/lyonbrown4d/regimux/internal/store/meta/sqlite"
)

func resolveSQLiteOpenConfig(opts DBOptions) (dbOpenConfig, error) {
	dsnSource := opts.DSN
	if strings.TrimSpace(dsnSource) == "" {
		dsnSource = opts.Path
	}
	dsn, err := metasqlite.DSN(dsnSource)
	if err != nil {
		return dbOpenConfig{}, errorf("%w: %v", ErrInvalidValue, err)
	}
	if err := metasqlite.EnsureDirectory(dsnSource); err != nil {
		return dbOpenConfig{}, wrapError(err, "prepare sqlite metadata directory")
	}
	return dbOpenConfig{driver: metaDriverSQLite, dsn: dsn, dialect: sqlitedialect.New()}, nil
}

func configureSQLitePragmas(ctx context.Context, raw *sql.DB) error {
	if err := metasqlite.ConfigurePragmas(ctx, raw); err != nil {
		return wrapError(err, "configure sqlite metadata pragma")
	}
	return nil
}
