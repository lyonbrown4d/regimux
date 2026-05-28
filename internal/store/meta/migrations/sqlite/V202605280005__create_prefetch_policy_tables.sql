CREATE TABLE IF NOT EXISTS "meta_prefetch_runs" (
	"id" BIGINT NOT NULL,
	"status" VARCHAR(32) NOT NULL,
	"trigger" VARCHAR(64) NOT NULL,
	"started_at" BIGINT NOT NULL,
	"finished_at" BIGINT NOT NULL,
	"scanned_records" INTEGER NOT NULL,
	"skipped_records" INTEGER NOT NULL,
	"repositories" INTEGER NOT NULL,
	"skipped_repositories" INTEGER NOT NULL,
	"candidates" INTEGER NOT NULL,
	"prefetched" INTEGER NOT NULL,
	"failed" INTEGER NOT NULL,
	"skipped_candidates" INTEGER NOT NULL,
	"bytes_warmed" BIGINT NOT NULL,
	"byte_budget" BIGINT NOT NULL,
	"task_budget" INTEGER NOT NULL,
	"repository_limit" INTEGER NOT NULL,
	"retry_requested" BOOLEAN NOT NULL,
	"error" TEXT NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_runs_status" ON "meta_prefetch_runs" ("status");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_runs_trigger" ON "meta_prefetch_runs" ("trigger");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_runs_started_at" ON "meta_prefetch_runs" ("started_at");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_runs_finished_at" ON "meta_prefetch_runs" ("finished_at");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_runs_created_at" ON "meta_prefetch_runs" ("created_at");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_runs_updated_at" ON "meta_prefetch_runs" ("updated_at");
CREATE TABLE IF NOT EXISTS "meta_prefetch_outcomes" (
	"id" BIGINT NOT NULL,
	"run_id" BIGINT NOT NULL,
	"candidate_key" VARCHAR(512) NOT NULL,
	"alias" VARCHAR(128) NOT NULL,
	"repository" VARCHAR(255) NOT NULL,
	"reference" VARCHAR(255) NOT NULL,
	"source_reference" VARCHAR(255) NOT NULL,
	"status" VARCHAR(32) NOT NULL,
	"reason" TEXT NOT NULL,
	"score" INTEGER NOT NULL,
	"manifest_digest" VARCHAR(128) NOT NULL,
	"layer_count" INTEGER NOT NULL,
	"blob_count" INTEGER NOT NULL,
	"child_manifest_count" INTEGER NOT NULL,
	"bytes_warmed" BIGINT NOT NULL,
	"attempt" INTEGER NOT NULL,
	"error" TEXT NOT NULL,
	"skip_reason" TEXT NOT NULL,
	"next_retry_at" BIGINT NOT NULL,
	"started_at" BIGINT NOT NULL,
	"finished_at" BIGINT NOT NULL,
	"created_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_run_id" ON "meta_prefetch_outcomes" ("run_id");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_candidate_key" ON "meta_prefetch_outcomes" ("candidate_key");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_alias" ON "meta_prefetch_outcomes" ("alias");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_repository" ON "meta_prefetch_outcomes" ("repository");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_reference" ON "meta_prefetch_outcomes" ("reference");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_source_reference" ON "meta_prefetch_outcomes" ("source_reference");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_status" ON "meta_prefetch_outcomes" ("status");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_manifest_digest" ON "meta_prefetch_outcomes" ("manifest_digest");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_next_retry_at" ON "meta_prefetch_outcomes" ("next_retry_at");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_started_at" ON "meta_prefetch_outcomes" ("started_at");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_finished_at" ON "meta_prefetch_outcomes" ("finished_at");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_created_at" ON "meta_prefetch_outcomes" ("created_at");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_outcomes_repo" ON "meta_prefetch_outcomes" ("alias", "repository");
CREATE TABLE IF NOT EXISTS "meta_prefetch_controls" (
	"id" BIGINT NOT NULL,
	"action" VARCHAR(32) NOT NULL,
	"reason" TEXT NOT NULL,
	"requested_at" BIGINT NOT NULL,
	"consumed_at" BIGINT NOT NULL,
	"created_at" BIGINT NOT NULL,
	"updated_at" BIGINT NOT NULL,
	PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_controls_action" ON "meta_prefetch_controls" ("action");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_controls_requested_at" ON "meta_prefetch_controls" ("requested_at");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_controls_consumed_at" ON "meta_prefetch_controls" ("consumed_at");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_controls_created_at" ON "meta_prefetch_controls" ("created_at");
CREATE INDEX IF NOT EXISTS "idx_meta_prefetch_controls_updated_at" ON "meta_prefetch_controls" ("updated_at");
