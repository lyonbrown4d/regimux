CREATE TABLE IF NOT EXISTS "meta_refresh_intents" (
	"id" BIGINT NOT NULL,
	"key" VARCHAR(128) NOT NULL,
	"ecosystem" VARCHAR(32) NOT NULL,
	"kind" VARCHAR(64) NOT NULL,
	"alias" VARCHAR(128) NOT NULL,
	"repository" VARCHAR(255) NOT NULL,
	"reference" VARCHAR(255) NOT NULL,
	"accept" TEXT NOT NULL,
	"due_at" BIGINT NOT NULL,
	"last_seen_at" BIGINT NOT NULL,
	"skipped" INTEGER NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "idx_meta_refresh_intents_key" ON "meta_refresh_intents" ("key");
CREATE INDEX IF NOT EXISTS "idx_meta_refresh_intents_ecosystem" ON "meta_refresh_intents" ("ecosystem");
CREATE INDEX IF NOT EXISTS "idx_meta_refresh_intents_kind" ON "meta_refresh_intents" ("kind");
CREATE INDEX IF NOT EXISTS "idx_meta_refresh_intents_alias" ON "meta_refresh_intents" ("alias");
CREATE INDEX IF NOT EXISTS "idx_meta_refresh_intents_repository" ON "meta_refresh_intents" ("repository");
CREATE INDEX IF NOT EXISTS "idx_meta_refresh_intents_reference" ON "meta_refresh_intents" ("reference");
CREATE INDEX IF NOT EXISTS "idx_meta_refresh_intents_due_at" ON "meta_refresh_intents" ("due_at");
CREATE INDEX IF NOT EXISTS "idx_meta_refresh_intents_last_seen_at" ON "meta_refresh_intents" ("last_seen_at");
CREATE INDEX IF NOT EXISTS "idx_meta_refresh_intents_created_at" ON "meta_refresh_intents" ("created_at");
CREATE INDEX IF NOT EXISTS "idx_meta_refresh_intents_updated_at" ON "meta_refresh_intents" ("updated_at");
CREATE INDEX IF NOT EXISTS "idx_meta_refresh_intents_target" ON "meta_refresh_intents" ("ecosystem", "alias", "repository", "reference");
