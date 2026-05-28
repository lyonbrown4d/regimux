CREATE TABLE IF NOT EXISTS "meta_endpoint_health" (
	"id" BIGINT NOT NULL,
	"key" VARCHAR(1024) NOT NULL,
	"alias" VARCHAR(128) NOT NULL,
	"registry" VARCHAR(512) NOT NULL,
	"repository" VARCHAR(255) NOT NULL,
	"latency_ewma" BIGINT NOT NULL,
	"latency_samples" BIGINT NOT NULL,
	"consecutive_failures" BIGINT NOT NULL,
	"success_count" BIGINT NOT NULL,
	"failure_count" BIGINT NOT NULL,
	"content_mismatch_count" BIGINT NOT NULL,
	"cooldown_until" BIGINT NOT NULL,
	"degraded_until" BIGINT NOT NULL,
	"last_success_at" BIGINT NOT NULL,
	"last_failure_at" BIGINT NOT NULL,
	"last_probe_at" BIGINT NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "idx_meta_endpoint_health_key" ON "meta_endpoint_health" ("key");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_alias" ON "meta_endpoint_health" ("alias");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_registry" ON "meta_endpoint_health" ("registry");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_repository" ON "meta_endpoint_health" ("repository");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_consecutive_failures" ON "meta_endpoint_health" ("consecutive_failures");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_cooldown_until" ON "meta_endpoint_health" ("cooldown_until");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_degraded_until" ON "meta_endpoint_health" ("degraded_until");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_last_success_at" ON "meta_endpoint_health" ("last_success_at");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_last_failure_at" ON "meta_endpoint_health" ("last_failure_at");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_last_probe_at" ON "meta_endpoint_health" ("last_probe_at");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_created_at" ON "meta_endpoint_health" ("created_at");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_updated_at" ON "meta_endpoint_health" ("updated_at");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_endpoint" ON "meta_endpoint_health" ("alias", "registry");
CREATE INDEX IF NOT EXISTS "idx_meta_endpoint_health_repo" ON "meta_endpoint_health" ("alias", "registry", "repository");
