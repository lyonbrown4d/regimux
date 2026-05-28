CREATE TABLE IF NOT EXISTS "meta_upstreams" (
	"id" BIGINT NOT NULL,
	"alias" VARCHAR(128) NOT NULL,
	"repository_count" BIGINT NOT NULL,
	"pull_count" BIGINT NOT NULL,
	"blob_bytes" BIGINT NOT NULL,
	"blob_link_count" BIGINT NOT NULL,
	"last_activity_at" BIGINT NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "idx_meta_upstreams_alias" ON "meta_upstreams" ("alias");
CREATE INDEX IF NOT EXISTS "idx_meta_upstreams_repository_count" ON "meta_upstreams" ("repository_count");
CREATE INDEX IF NOT EXISTS "idx_meta_upstreams_pull_count" ON "meta_upstreams" ("pull_count");
CREATE INDEX IF NOT EXISTS "idx_meta_upstreams_blob_bytes" ON "meta_upstreams" ("blob_bytes");
CREATE INDEX IF NOT EXISTS "idx_meta_upstreams_blob_link_count" ON "meta_upstreams" ("blob_link_count");
CREATE INDEX IF NOT EXISTS "idx_meta_upstreams_last_activity_at" ON "meta_upstreams" ("last_activity_at");
CREATE INDEX IF NOT EXISTS "idx_meta_upstreams_created_at" ON "meta_upstreams" ("created_at");
CREATE INDEX IF NOT EXISTS "idx_meta_upstreams_updated_at" ON "meta_upstreams" ("updated_at");
CREATE TABLE IF NOT EXISTS "meta_repositories" (
	"id" BIGINT NOT NULL,
	"key" VARCHAR(512) NOT NULL,
	"upstream_id" BIGINT NOT NULL,
	"alias" VARCHAR(128) NOT NULL,
	"name" VARCHAR(255) NOT NULL,
	"pull_count" BIGINT NOT NULL,
	"blob_bytes" BIGINT NOT NULL,
	"blob_link_count" BIGINT NOT NULL,
	"last_pull_at" BIGINT NOT NULL,
	"last_blob_access_at" BIGINT NOT NULL,
	"last_activity_at" BIGINT NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "idx_meta_repositories_key" ON "meta_repositories" ("key");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_upstream_id" ON "meta_repositories" ("upstream_id");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_alias" ON "meta_repositories" ("alias");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_name" ON "meta_repositories" ("name");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_pull_count" ON "meta_repositories" ("pull_count");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_blob_bytes" ON "meta_repositories" ("blob_bytes");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_blob_link_count" ON "meta_repositories" ("blob_link_count");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_last_pull_at" ON "meta_repositories" ("last_pull_at");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_last_blob_access_at" ON "meta_repositories" ("last_blob_access_at");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_last_activity_at" ON "meta_repositories" ("last_activity_at");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_created_at" ON "meta_repositories" ("created_at");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_updated_at" ON "meta_repositories" ("updated_at");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_upstream_name" ON "meta_repositories" ("upstream_id", "name");
