package meta

import (
	"context"
	"database/sql"
	"embed"

	"github.com/arcgolabs/dbx"
	dbxdialect "github.com/arcgolabs/dbx/dialect"
	"github.com/arcgolabs/dbx/migrate"
	mysql "github.com/go-sql-driver/mysql"
)

const metadataMigrationHistoryTable = "meta_schema_history"

//go:embed migrations/*/*.sql
var metadataMigrationFS embed.FS

func runDBMigrations(ctx context.Context, db *dbx.DB) error {
	return runMigrations(ctx, db.SQLDB(), db.Dialect())
}

func runMySQLMigrations(ctx context.Context, dsn string, dialect dbxdialect.Dialect) (resultErr error) {
	config, err := mysqlMigrationConfig(dsn)
	if err != nil {
		return err
	}
	connector, err := mysql.NewConnector(config)
	if err != nil {
		return wrapError(err, "configure mysql migration connector")
	}
	migrationDB := sql.OpenDB(connector)
	migrationDB.SetMaxOpenConns(2)
	migrationDB.SetMaxIdleConns(2)
	resultErr = runMigrations(ctx, migrationDB, dialect)
	closeErr := migrationDB.Close()
	if closeErr != nil {
		return joinError("run mysql metadata migrations and close migration db",
			resultErr, wrapError(closeErr, "close mysql migration db"))
	}
	return resultErr
}

func mysqlMigrationConfig(dsn string) (*mysql.Config, error) {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, wrapError(err, "parse mysql metadata dsn for migrations")
	}
	config.MultiStatements = true
	return config, nil
}

func runMigrations(ctx context.Context, db *sql.DB, dialect dbxdialect.Dialect) error {
	locker, enabled, err := metadataMigrationLocker(dialect.Name())
	if err != nil {
		return err
	}
	if !enabled {
		return runMigrationsUnlocked(ctx, db, dialect)
	}
	return withMetadataMigrationLock(ctx, db, locker, func() error {
		return runMigrationsUnlocked(ctx, db, dialect)
	})
}

func runMigrationsUnlocked(ctx context.Context, db *sql.DB, dialect dbxdialect.Dialect) error {
	runner := migrate.NewRunner(db, dialect, migrate.RunnerOptions{
		HistoryTable: metadataMigrationHistoryTable,
		ValidateHash: true,
	})
	source, err := metadataMigrationSource(dialect.Name())
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
