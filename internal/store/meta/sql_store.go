// Package meta stores registry metadata records.
package meta

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	"github.com/arcgolabs/dbx"
	dbxdialect "github.com/arcgolabs/dbx/dialect"
	mysqldialect "github.com/arcgolabs/dbx/dialect/mysql"
	postgresdialect "github.com/arcgolabs/dbx/dialect/postgres"
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
	Hooks  []dbx.Hook
	Debug  bool
}

type SQLStore struct {
	driver           string
	db               *dbx.DB
	mapper           *MetadataMapper
	upstreams        *repository.Base[upstreamRow, upstreamRowSchema]
	repositories     *repository.Base[repositoryRow, repositoryRowSchema]
	manifest         *repository.Base[manifestRow, manifestRowSchema]
	tags             *repository.Base[tagRow, tagRowSchema]
	pulls            *repository.Base[pullRow, pullRowSchema]
	blobs            *repository.Base[blobRow, blobRowSchema]
	repoBlobs        *repository.Base[repoBlobRow, repoBlobRowSchema]
	endpointHealth   *repository.Base[endpointHealthRow, endpointHealthRowSchema]
	prefetchRuns     *repository.Base[prefetchRunRow, prefetchRunRowSchema]
	prefetchOutcomes *repository.Base[prefetchOutcomeRow, prefetchOutcomeRowSchema]
	prefetchControls *repository.Base[prefetchControlRow, prefetchControlRowSchema]
}

func OpenSQLite(path string, logger *slog.Logger) (*SQLStore, error) {
	return OpenDBWithOptions(context.Background(), DBOptions{
		Driver: metaDriverSQLite,
		Path:   path,
		Logger: logger,
	})
}

func OpenSQLiteWithOptions(ctx context.Context, opts DBOptions) (*SQLStore, error) {
	opts.Driver = metaDriverSQLite
	return OpenDBWithOptions(ctx, opts)
}

func OpenDBWithOptions(ctx context.Context, opts DBOptions) (*SQLStore, error) {
	core, err := OpenMetadataDB(ctx, opts)
	if err != nil {
		return nil, err
	}
	return NewSQLStore(core, NewMetadataMapper()), nil
}

func OpenMetadataDB(ctx context.Context, opts DBOptions) (*dbx.DB, error) {
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
		dbx.ApplyOptions(dbOptions(opts)...),
	)
	if err != nil {
		return nil, wrapError(err, "open metadata db")
	}
	configureMetadataDBPool(core.SQLDB(), openConfig.driver)
	if openConfig.driver == metaDriverSQLite {
		err = configureSQLitePragmas(ctx, core.SQLDB())
	}
	if err != nil {
		closeErr := core.Close()
		if closeErr != nil {
			return nil, joinError("configure metadata db and close after failure", err, wrapError(closeErr, "close metadata db"))
		}
		return nil, err
	}

	if err := MigrateMetadataDB(ctx, core); err != nil {
		closeErr := core.Close()
		if closeErr != nil {
			return nil, joinError("migrate metadata db and close after failure", err, wrapError(closeErr, "close metadata db"))
		}
		return nil, err
	}
	return core, nil
}

func NewSQLStore(db *dbx.DB, mapper *MetadataMapper) *SQLStore {
	if mapper == nil {
		mapper = NewMetadataMapper()
	}
	return &SQLStore{
		driver:           normalizeDBDriver(db.Dialect().Name()),
		db:               db,
		mapper:           mapper,
		upstreams:        repository.New[upstreamRow](db, sqlUpstreamRows),
		repositories:     repository.New[repositoryRow](db, sqlRepositoryRows),
		manifest:         repository.New[manifestRow](db, sqlManifestRows),
		tags:             repository.New[tagRow](db, sqlTagRows),
		pulls:            repository.New[pullRow](db, sqlPullRows),
		blobs:            repository.New[blobRow](db, sqlBlobRows),
		repoBlobs:        repository.New[repoBlobRow](db, sqlRepoBlobRows),
		endpointHealth:   repository.New[endpointHealthRow](db, sqlEndpointHealthRows),
		prefetchRuns:     repository.New[prefetchRunRow](db, sqlPrefetchRunRows),
		prefetchOutcomes: repository.New[prefetchOutcomeRow](db, sqlPrefetchOutcomeRows),
		prefetchControls: repository.New[prefetchControlRow](db, sqlPrefetchControlRows),
	}
}

func dbOptions(opts DBOptions) []dbx.Option {
	dbOpts := []dbx.Option{
		dbx.WithLogger(opts.Logger),
		dbx.WithDebug(opts.Debug),
	}
	if len(opts.Hooks) > 0 {
		dbOpts = append(dbOpts, dbx.WithHooks(opts.Hooks...))
	}
	return dbOpts
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

func configureMetadataDBPool(raw *sql.DB, driver string) {
	if raw == nil {
		return
	}
	if driver == metaDriverSQLite {
		raw.SetMaxOpenConns(1)
		raw.SetMaxIdleConns(1)
		raw.SetConnMaxLifetime(0)
		return
	}
	raw.SetMaxOpenConns(8)
	raw.SetMaxIdleConns(8)
	raw.SetConnMaxLifetime(0)
}

func MigrateMetadataDB(ctx context.Context, db *dbx.DB) error {
	if err := requireMetadataContext(ctx, "migrate metadata store"); err != nil {
		return err
	}
	if err := runDBMigrations(ctx, db); err != nil {
		return wrapError(err, "migrate metadata schema")
	}
	return nil
}

func (s *SQLStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return wrapError(err, "close metadata db")
	}
	return nil
}

func metadataNow() time.Time {
	return time.Now().UTC()
}

var _ Store = (*SQLStore)(nil)
