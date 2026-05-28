package meta

import (
	"context"
	"database/sql"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dbx/migrate"
)

const sqliteMigrationHistoryTable = "meta_schema_history"

func runDBMigrations(ctx context.Context, db *dbx.DB) error {
	runner := migrate.NewRunner(db.SQLDB(), db.Dialect(), migrate.RunnerOptions{
		HistoryTable: sqliteMigrationHistoryTable,
		ValidateHash: true,
	})
	_, err := runner.UpGo(ctx, migrate.NewGoMigration(
		"202605280001",
		"create metadata tables",
		func(ctx context.Context, tx *sql.Tx) error {
			return createMetadataTables(ctx, tx, db.Dialect().Name())
		},
		nil,
	))
	if err != nil {
		return wrapError(err, "run metadata migrations")
	}
	return nil
}

func createMetadataTables(ctx context.Context, tx *sql.Tx, driver string) error {
	statements, err := metadataMigrationSQL(driver)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return wrapError(err, "create metadata table")
		}
	}
	return nil
}

func metadataMigrationSQL(driver string) ([]string, error) {
	switch normalizeDBDriver(driver) {
	case metaDriverSQLite, metaDriverPostgres:
		return quotedMetadataMigrationSQL, nil
	case metaDriverMySQL:
		return mysqlMetadataMigrationSQL, nil
	default:
		return nil, errorf("%w: metadata migration driver must be sqlite, mysql, or postgres", ErrInvalidValue)
	}
}

var quotedMetadataMigrationSQL = []string{
	`CREATE TABLE IF NOT EXISTS "meta_manifests" (
	"id" BIGINT NOT NULL,
	"key" VARCHAR(512) NOT NULL,
	"alias" VARCHAR(128) NOT NULL,
	"repository" VARCHAR(255) NOT NULL,
	"reference" VARCHAR(255) NOT NULL,
	"accept_key" VARCHAR(512) NOT NULL,
	"digest" VARCHAR(128) NOT NULL,
	"media_type" VARCHAR(255) NOT NULL,
	"size" BIGINT NOT NULL,
	"object_key" VARCHAR(512) NOT NULL,
	"headers" TEXT NOT NULL,
	"expires_at" BIGINT NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS "idx_meta_manifests_key" ON "meta_manifests" ("key")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_manifests_alias" ON "meta_manifests" ("alias")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_manifests_repository" ON "meta_manifests" ("repository")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_manifests_reference" ON "meta_manifests" ("reference")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_manifests_accept_key" ON "meta_manifests" ("accept_key")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_manifests_digest" ON "meta_manifests" ("digest")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_manifests_expires_at" ON "meta_manifests" ("expires_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_manifests_created_at" ON "meta_manifests" ("created_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_manifests_updated_at" ON "meta_manifests" ("updated_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_manifests_repo" ON "meta_manifests" ("alias", "repository")`,
	`CREATE TABLE IF NOT EXISTS "meta_tags" (
	"id" BIGINT NOT NULL,
	"key" VARCHAR(512) NOT NULL,
	"alias" VARCHAR(128) NOT NULL,
	"repository" VARCHAR(255) NOT NULL,
	"reference" VARCHAR(255) NOT NULL,
	"digest" VARCHAR(128) NOT NULL,
	"expires_at" BIGINT NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS "idx_meta_tags_key" ON "meta_tags" ("key")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_tags_alias" ON "meta_tags" ("alias")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_tags_repository" ON "meta_tags" ("repository")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_tags_reference" ON "meta_tags" ("reference")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_tags_digest" ON "meta_tags" ("digest")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_tags_expires_at" ON "meta_tags" ("expires_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_tags_created_at" ON "meta_tags" ("created_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_tags_updated_at" ON "meta_tags" ("updated_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_tags_repo" ON "meta_tags" ("alias", "repository")`,
	`CREATE TABLE IF NOT EXISTS "meta_pulls" (
	"id" BIGINT NOT NULL,
	"key" VARCHAR(512) NOT NULL,
	"alias" VARCHAR(128) NOT NULL,
	"repository" VARCHAR(255) NOT NULL,
	"reference" VARCHAR(255) NOT NULL,
	"count" BIGINT NOT NULL,
	"last_pull_at" BIGINT NOT NULL,
	"last_upstream_pull_at" BIGINT NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS "idx_meta_pulls_key" ON "meta_pulls" ("key")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_pulls_alias" ON "meta_pulls" ("alias")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_pulls_repository" ON "meta_pulls" ("repository")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_pulls_reference" ON "meta_pulls" ("reference")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_pulls_count" ON "meta_pulls" ("count")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_pulls_last_pull_at" ON "meta_pulls" ("last_pull_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_pulls_last_upstream_pull_at" ON "meta_pulls" ("last_upstream_pull_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_pulls_created_at" ON "meta_pulls" ("created_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_pulls_updated_at" ON "meta_pulls" ("updated_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_pulls_repo" ON "meta_pulls" ("alias", "repository")`,
	`CREATE TABLE IF NOT EXISTS "meta_blobs" (
	"id" BIGINT NOT NULL,
	"digest" VARCHAR(128) NOT NULL,
	"size" BIGINT NOT NULL,
	"media_type" VARCHAR(255) NOT NULL,
	"object_key" VARCHAR(512) NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	"last_access_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS "idx_meta_blobs_digest" ON "meta_blobs" ("digest")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_blobs_size" ON "meta_blobs" ("size")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_blobs_created_at" ON "meta_blobs" ("created_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_blobs_updated_at" ON "meta_blobs" ("updated_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_blobs_last_access_at" ON "meta_blobs" ("last_access_at")`,
	`CREATE TABLE IF NOT EXISTS "meta_repo_blobs" (
	"id" BIGINT NOT NULL,
	"key" VARCHAR(512) NOT NULL,
	"alias" VARCHAR(128) NOT NULL,
	"repository" VARCHAR(255) NOT NULL,
	"digest" VARCHAR(128) NOT NULL,
	"source_manifest" VARCHAR(128) NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	"last_access_at" BIGINT NOT NULL,
	"last_verified_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS "idx_meta_repo_blobs_key" ON "meta_repo_blobs" ("key")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_repo_blobs_alias" ON "meta_repo_blobs" ("alias")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_repo_blobs_repository" ON "meta_repo_blobs" ("repository")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_repo_blobs_digest" ON "meta_repo_blobs" ("digest")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_repo_blobs_source_manifest" ON "meta_repo_blobs" ("source_manifest")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_repo_blobs_created_at" ON "meta_repo_blobs" ("created_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_repo_blobs_updated_at" ON "meta_repo_blobs" ("updated_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_repo_blobs_last_access_at" ON "meta_repo_blobs" ("last_access_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_repo_blobs_last_verified_at" ON "meta_repo_blobs" ("last_verified_at")`,
	`CREATE INDEX IF NOT EXISTS "idx_meta_repo_blobs_repo" ON "meta_repo_blobs" ("alias", "repository")`,
}

var mysqlMetadataMigrationSQL = []string{
	`CREATE TABLE IF NOT EXISTS ` + "`meta_manifests`" + ` (
	` + "`id`" + ` BIGINT NOT NULL,
	` + "`key`" + ` VARCHAR(512) NOT NULL,
	` + "`alias`" + ` VARCHAR(128) NOT NULL,
	` + "`repository`" + ` VARCHAR(255) NOT NULL,
	` + "`reference`" + ` VARCHAR(255) NOT NULL,
	` + "`accept_key`" + ` VARCHAR(512) NOT NULL,
	` + "`digest`" + ` VARCHAR(128) NOT NULL,
	` + "`media_type`" + ` VARCHAR(255) NOT NULL,
	` + "`size`" + ` BIGINT NOT NULL,
	` + "`object_key`" + ` VARCHAR(512) NOT NULL,
	` + "`headers`" + ` TEXT NOT NULL,
	` + "`expires_at`" + ` BIGINT NOT NULL,
	` + "`created_at`" + ` BIGINT NOT NULL,
	` + "`updated_at`" + ` BIGINT NOT NULL,
	PRIMARY KEY (` + "`id`" + `),
	UNIQUE KEY ` + "`idx_meta_manifests_key`" + ` (` + "`key`" + `),
	KEY ` + "`idx_meta_manifests_alias`" + ` (` + "`alias`" + `),
	KEY ` + "`idx_meta_manifests_repository`" + ` (` + "`repository`" + `),
	KEY ` + "`idx_meta_manifests_reference`" + ` (` + "`reference`" + `),
	KEY ` + "`idx_meta_manifests_accept_key`" + ` (` + "`accept_key`" + `),
	KEY ` + "`idx_meta_manifests_digest`" + ` (` + "`digest`" + `),
	KEY ` + "`idx_meta_manifests_expires_at`" + ` (` + "`expires_at`" + `),
	KEY ` + "`idx_meta_manifests_created_at`" + ` (` + "`created_at`" + `),
	KEY ` + "`idx_meta_manifests_updated_at`" + ` (` + "`updated_at`" + `),
	KEY ` + "`idx_meta_manifests_repo`" + ` (` + "`alias`" + `, ` + "`repository`" + `)
)`,
	`CREATE TABLE IF NOT EXISTS ` + "`meta_tags`" + ` (
	` + "`id`" + ` BIGINT NOT NULL,
	` + "`key`" + ` VARCHAR(512) NOT NULL,
	` + "`alias`" + ` VARCHAR(128) NOT NULL,
	` + "`repository`" + ` VARCHAR(255) NOT NULL,
	` + "`reference`" + ` VARCHAR(255) NOT NULL,
	` + "`digest`" + ` VARCHAR(128) NOT NULL,
	` + "`expires_at`" + ` BIGINT NOT NULL,
	` + "`created_at`" + ` BIGINT NOT NULL,
	` + "`updated_at`" + ` BIGINT NOT NULL,
	PRIMARY KEY (` + "`id`" + `),
	UNIQUE KEY ` + "`idx_meta_tags_key`" + ` (` + "`key`" + `),
	KEY ` + "`idx_meta_tags_alias`" + ` (` + "`alias`" + `),
	KEY ` + "`idx_meta_tags_repository`" + ` (` + "`repository`" + `),
	KEY ` + "`idx_meta_tags_reference`" + ` (` + "`reference`" + `),
	KEY ` + "`idx_meta_tags_digest`" + ` (` + "`digest`" + `),
	KEY ` + "`idx_meta_tags_expires_at`" + ` (` + "`expires_at`" + `),
	KEY ` + "`idx_meta_tags_created_at`" + ` (` + "`created_at`" + `),
	KEY ` + "`idx_meta_tags_updated_at`" + ` (` + "`updated_at`" + `),
	KEY ` + "`idx_meta_tags_repo`" + ` (` + "`alias`" + `, ` + "`repository`" + `)
)`,
	`CREATE TABLE IF NOT EXISTS ` + "`meta_pulls`" + ` (
	` + "`id`" + ` BIGINT NOT NULL,
	` + "`key`" + ` VARCHAR(512) NOT NULL,
	` + "`alias`" + ` VARCHAR(128) NOT NULL,
	` + "`repository`" + ` VARCHAR(255) NOT NULL,
	` + "`reference`" + ` VARCHAR(255) NOT NULL,
	` + "`count`" + ` BIGINT NOT NULL,
	` + "`last_pull_at`" + ` BIGINT NOT NULL,
	` + "`last_upstream_pull_at`" + ` BIGINT NOT NULL,
	` + "`created_at`" + ` BIGINT NOT NULL,
	` + "`updated_at`" + ` BIGINT NOT NULL,
	PRIMARY KEY (` + "`id`" + `),
	UNIQUE KEY ` + "`idx_meta_pulls_key`" + ` (` + "`key`" + `),
	KEY ` + "`idx_meta_pulls_alias`" + ` (` + "`alias`" + `),
	KEY ` + "`idx_meta_pulls_repository`" + ` (` + "`repository`" + `),
	KEY ` + "`idx_meta_pulls_reference`" + ` (` + "`reference`" + `),
	KEY ` + "`idx_meta_pulls_count`" + ` (` + "`count`" + `),
	KEY ` + "`idx_meta_pulls_last_pull_at`" + ` (` + "`last_pull_at`" + `),
	KEY ` + "`idx_meta_pulls_last_upstream_pull_at`" + ` (` + "`last_upstream_pull_at`" + `),
	KEY ` + "`idx_meta_pulls_created_at`" + ` (` + "`created_at`" + `),
	KEY ` + "`idx_meta_pulls_updated_at`" + ` (` + "`updated_at`" + `),
	KEY ` + "`idx_meta_pulls_repo`" + ` (` + "`alias`" + `, ` + "`repository`" + `)
)`,
	`CREATE TABLE IF NOT EXISTS ` + "`meta_blobs`" + ` (
	` + "`id`" + ` BIGINT NOT NULL,
	` + "`digest`" + ` VARCHAR(128) NOT NULL,
	` + "`size`" + ` BIGINT NOT NULL,
	` + "`media_type`" + ` VARCHAR(255) NOT NULL,
	` + "`object_key`" + ` VARCHAR(512) NOT NULL,
	` + "`created_at`" + ` BIGINT NOT NULL,
	` + "`updated_at`" + ` BIGINT NOT NULL,
	` + "`last_access_at`" + ` BIGINT NOT NULL,
	PRIMARY KEY (` + "`id`" + `),
	UNIQUE KEY ` + "`idx_meta_blobs_digest`" + ` (` + "`digest`" + `),
	KEY ` + "`idx_meta_blobs_size`" + ` (` + "`size`" + `),
	KEY ` + "`idx_meta_blobs_created_at`" + ` (` + "`created_at`" + `),
	KEY ` + "`idx_meta_blobs_updated_at`" + ` (` + "`updated_at`" + `),
	KEY ` + "`idx_meta_blobs_last_access_at`" + ` (` + "`last_access_at`" + `)
)`,
	`CREATE TABLE IF NOT EXISTS ` + "`meta_repo_blobs`" + ` (
	` + "`id`" + ` BIGINT NOT NULL,
	` + "`key`" + ` VARCHAR(512) NOT NULL,
	` + "`alias`" + ` VARCHAR(128) NOT NULL,
	` + "`repository`" + ` VARCHAR(255) NOT NULL,
	` + "`digest`" + ` VARCHAR(128) NOT NULL,
	` + "`source_manifest`" + ` VARCHAR(128) NOT NULL,
	` + "`created_at`" + ` BIGINT NOT NULL,
	` + "`updated_at`" + ` BIGINT NOT NULL,
	` + "`last_access_at`" + ` BIGINT NOT NULL,
	` + "`last_verified_at`" + ` BIGINT NOT NULL,
	PRIMARY KEY (` + "`id`" + `),
	UNIQUE KEY ` + "`idx_meta_repo_blobs_key`" + ` (` + "`key`" + `),
	KEY ` + "`idx_meta_repo_blobs_alias`" + ` (` + "`alias`" + `),
	KEY ` + "`idx_meta_repo_blobs_repository`" + ` (` + "`repository`" + `),
	KEY ` + "`idx_meta_repo_blobs_digest`" + ` (` + "`digest`" + `),
	KEY ` + "`idx_meta_repo_blobs_source_manifest`" + ` (` + "`source_manifest`" + `),
	KEY ` + "`idx_meta_repo_blobs_created_at`" + ` (` + "`created_at`" + `),
	KEY ` + "`idx_meta_repo_blobs_updated_at`" + ` (` + "`updated_at`" + `),
	KEY ` + "`idx_meta_repo_blobs_last_access_at`" + ` (` + "`last_access_at`" + `),
	KEY ` + "`idx_meta_repo_blobs_last_verified_at`" + ` (` + "`last_verified_at`" + `),
	KEY ` + "`idx_meta_repo_blobs_repo`" + ` (` + "`alias`" + `, ` + "`repository`" + `)
)`,
}
