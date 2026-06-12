ALTER TABLE "meta_pulls"
	ADD COLUMN "policy_denied_count" BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN "last_policy_denied_at" BIGINT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS "idx_meta_pulls_policy_denied_count" ON "meta_pulls" ("policy_denied_count");
CREATE INDEX IF NOT EXISTS "idx_meta_pulls_last_policy_denied_at" ON "meta_pulls" ("last_policy_denied_at");

ALTER TABLE "meta_repositories"
	ADD COLUMN "policy_denied_pull_count" BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN "last_policy_denied_at" BIGINT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_policy_denied_pull_count" ON "meta_repositories" ("policy_denied_pull_count");
CREATE INDEX IF NOT EXISTS "idx_meta_repositories_last_policy_denied_at" ON "meta_repositories" ("last_policy_denied_at");

ALTER TABLE "meta_upstreams"
	ADD COLUMN "policy_denied_pull_count" BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN "last_policy_denied_at" BIGINT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS "idx_meta_upstreams_policy_denied_pull_count" ON "meta_upstreams" ("policy_denied_pull_count");
CREATE INDEX IF NOT EXISTS "idx_meta_upstreams_last_policy_denied_at" ON "meta_upstreams" ("last_policy_denied_at");
