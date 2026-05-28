package meta

import (
	"context"
	"embed"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/migrate"
)

const metadataMigrationHistoryTable = "meta_schema_history"

//go:embed migrations/*/*.sql
var metadataMigrationFS embed.FS

func runDBMigrations(ctx context.Context, db *dbx.DB) error {
	runner := migrate.NewRunner(db.SQLDB(), db.Dialect(), migrate.RunnerOptions{
		HistoryTable: metadataMigrationHistoryTable,
		ValidateHash: true,
	})
	source, err := metadataMigrationSource(db.Dialect().Name())
	if err != nil {
		return err
	}
	_, err = runner.UpSQL(ctx, source)
	if err != nil {
		return wrapError(err, "run metadata migrations")
	}
	return nil
}

func metadataMigrationSource(driver string) (migrate.FileSource, error) {
	switch normalizeDBDriver(driver) {
	case metaDriverSQLite:
		return migrate.FileSource{FS: metadataMigrationFS, Dir: "migrations/sqlite"}, nil
	case metaDriverMySQL:
		return migrate.FileSource{FS: metadataMigrationFS, Dir: "migrations/mysql"}, nil
	case metaDriverPostgres:
		return migrate.FileSource{FS: metadataMigrationFS, Dir: "migrations/postgres"}, nil
	default:
		return migrate.FileSource{}, errorf("%w: metadata migration driver must be sqlite, mysql, or postgres", ErrInvalidValue)
	}
}
