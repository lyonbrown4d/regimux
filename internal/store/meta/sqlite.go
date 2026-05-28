// Package meta stores registry metadata records.
package meta

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arcgolabs/dbx"
	dbxdialect "github.com/arcgolabs/dbx/dialect"
	mysqldialect "github.com/arcgolabs/dbx/dialect/mysql"
	postgresdialect "github.com/arcgolabs/dbx/dialect/postgres"
	sqlitedialect "github.com/arcgolabs/dbx/dialect/sqlite"
	"github.com/arcgolabs/dbx/repository"

	_ "github.com/go-sql-driver/mysql" // register the database/sql MySQL driver.
	_ "github.com/lib/pq"              // register the database/sql PostgreSQL driver.
	_ "modernc.org/sqlite"             // register the database/sql SQLite driver.
)

const (
	metaDriverMySQL    = "mysql"
	metaDriverPostgres = "postgres"
	metaDriverSQLite   = "sqlite"
)

type DBOptions struct {
	Driver string
	DSN    string
	Path   string
	Logger *slog.Logger
}

type SQLiteOptions = DBOptions

type SQLiteStore struct {
	driver    string
	db        *dbx.DB
	manifest  *repository.Base[manifestRow, manifestRowSchema]
	tags      *repository.Base[tagRow, tagRowSchema]
	pulls     *repository.Base[pullRow, pullRowSchema]
	blobs     *repository.Base[blobRow, blobRowSchema]
	repoBlobs *repository.Base[repoBlobRow, repoBlobRowSchema]
}

func OpenSQLite(path string, logger *slog.Logger) (*SQLiteStore, error) {
	return OpenDBWithOptions(context.Background(), DBOptions{
		Driver: metaDriverSQLite,
		Path:   path,
		Logger: logger,
	})
}

func OpenSQLiteWithOptions(ctx context.Context, opts SQLiteOptions) (*SQLiteStore, error) {
	opts.Driver = metaDriverSQLite
	return OpenDBWithOptions(ctx, opts)
}

func OpenDBWithOptions(ctx context.Context, opts DBOptions) (*SQLiteStore, error) {
	if err := requireMetadataContext(ctx, "open metadata store"); err != nil {
		return nil, err
	}

	openConfig, err := resolveDBOpenConfig(opts)
	if err != nil {
		return nil, err
	}

	core, err := dbx.Open(
		dbx.WithDriver(openConfig.driver),
		dbx.WithDSN(openConfig.dsn),
		dbx.WithDialect(openConfig.dialect),
		dbx.ApplyOptions(dbx.WithLogger(opts.Logger)),
	)
	if err != nil {
		return nil, wrapError(err, "open metadata db")
	}
	configureSQLitePool(core.SQLDB())
	if openConfig.driver == metaDriverSQLite {
		err = configureSQLitePragmas(ctx, core.SQLDB())
	}
	if err != nil {
		closeErr := core.Close()
		if closeErr != nil {
			return nil, errors.Join(err, wrapError(closeErr, "close metadata db"))
		}
		return nil, err
	}

	store := &SQLiteStore{
		driver:    openConfig.driver,
		db:        core,
		manifest:  repository.New[manifestRow](core, sqliteManifestRows),
		tags:      repository.New[tagRow](core, sqliteTagRows),
		pulls:     repository.New[pullRow](core, sqlitePullRows),
		blobs:     repository.New[blobRow](core, sqliteBlobRows),
		repoBlobs: repository.New[repoBlobRow](core, sqliteRepoBlobRows),
	}
	if err := store.migrate(ctx); err != nil {
		closeErr := core.Close()
		if closeErr != nil {
			return nil, errors.Join(err, wrapError(closeErr, "close metadata db"))
		}
		return nil, err
	}
	return store, nil
}

type dbOpenConfig struct {
	driver  string
	dsn     string
	dialect dbxdialect.Dialect
}

func resolveDBOpenConfig(opts DBOptions) (dbOpenConfig, error) {
	driver := normalizeDBDriver(opts.Driver)
	switch driver {
	case metaDriverSQLite:
		return resolveSQLiteOpenConfig(opts)
	case metaDriverMySQL:
		return resolveExternalOpenConfig(driver, opts.DSN, mysqldialect.New())
	case metaDriverPostgres:
		return resolveExternalOpenConfig(driver, opts.DSN, postgresdialect.New())
	default:
		return dbOpenConfig{}, errorf("%w: metadata store driver must be sqlite, mysql, or postgres", ErrInvalidValue)
	}
}

func resolveSQLiteOpenConfig(opts DBOptions) (dbOpenConfig, error) {
	dsnSource := opts.DSN
	if strings.TrimSpace(dsnSource) == "" {
		dsnSource = opts.Path
	}
	dsn, err := sqliteDSN(dsnSource)
	if err != nil {
		return dbOpenConfig{}, err
	}
	if err := ensureSQLiteDirectory(dsnSource); err != nil {
		return dbOpenConfig{}, err
	}
	return dbOpenConfig{driver: metaDriverSQLite, dsn: dsn, dialect: sqlitedialect.New()}, nil
}

func resolveExternalOpenConfig(driver, dsn string, dialect dbxdialect.Dialect) (dbOpenConfig, error) {
	dsn, err := requiredMetadataDSN(driver, dsn)
	if err != nil {
		return dbOpenConfig{}, err
	}
	return dbOpenConfig{driver: driver, dsn: dsn, dialect: dialect}, nil
}

func normalizeDBDriver(driver string) string {
	driver = strings.ToLower(strings.TrimSpace(driver))
	switch driver {
	case "":
		return metaDriverSQLite
	case "pg", "postgresql":
		return metaDriverPostgres
	default:
		return driver
	}
}

func requiredMetadataDSN(driver, dsn string) (string, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return "", errorf("%w: %s metadata dsn is required", ErrInvalidValue, driver)
	}
	return dsn, nil
}

func requireMetadataContext(ctx context.Context, operation string) error {
	if ctx == nil {
		return errorf("%w: %s context is required", ErrInvalidValue, operation)
	}
	if err := ctx.Err(); err != nil {
		return wrapError(err, "%s context", operation)
	}
	return nil
}

func sqliteDSN(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errorf("%w: sqlite path is required", ErrInvalidValue)
	}
	if strings.EqualFold(path, ":memory:") || strings.HasPrefix(strings.ToLower(path), "file:") {
		return path, nil
	}
	return filepath.Clean(path), nil
}

func ensureSQLiteDirectory(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || strings.EqualFold(path, ":memory:") || strings.HasPrefix(strings.ToLower(path), "file:") {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(path)), 0o750); err != nil {
		return wrapError(err, "create sqlite metadata directory")
	}
	return nil
}

func configureSQLitePool(raw *sql.DB) {
	if raw == nil {
		return
	}
	raw.SetMaxOpenConns(8)
	raw.SetMaxIdleConns(8)
	raw.SetConnMaxLifetime(0)
}

func configureSQLitePragmas(ctx context.Context, raw *sql.DB) error {
	if raw == nil {
		return errorf("%w: sqlite db is required", ErrInvalidValue)
	}
	statements := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}
	for _, statement := range statements {
		if _, err := raw.ExecContext(ctx, statement); err != nil {
			return wrapError(err, "configure sqlite metadata pragma")
		}
	}
	return nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	if err := requireMetadataContext(ctx, "migrate sqlite metadata store"); err != nil {
		return err
	}
	if err := runDBMigrations(ctx, s.db); err != nil {
		return wrapError(err, "migrate metadata schema")
	}
	return nil
}

func (s *SQLiteStore) UpstreamByAlias(context.Context, string) (*Upstream, error) {
	return nil, ErrNotFound
}

func (s *SQLiteStore) RepositoryByName(context.Context, int64, string) (*Repository, error) {
	return nil, ErrNotFound
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return wrapError(err, "close metadata db")
	}
	return nil
}

func sqliteNow() time.Time {
	return time.Now().UTC()
}

var _ Store = (*SQLiteStore)(nil)
