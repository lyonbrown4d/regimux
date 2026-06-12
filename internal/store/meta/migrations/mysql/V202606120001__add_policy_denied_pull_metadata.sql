ALTER TABLE `meta_pulls`
	ADD COLUMN `policy_denied_count` BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN `last_policy_denied_at` BIGINT NOT NULL DEFAULT 0,
	ADD KEY `idx_meta_pulls_policy_denied_count` (`policy_denied_count`),
	ADD KEY `idx_meta_pulls_last_policy_denied_at` (`last_policy_denied_at`);

ALTER TABLE `meta_repositories`
	ADD COLUMN `policy_denied_pull_count` BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN `last_policy_denied_at` BIGINT NOT NULL DEFAULT 0,
	ADD KEY `idx_meta_repositories_policy_denied_pull_count` (`policy_denied_pull_count`),
	ADD KEY `idx_meta_repositories_last_policy_denied_at` (`last_policy_denied_at`);

ALTER TABLE `meta_upstreams`
	ADD COLUMN `policy_denied_pull_count` BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN `last_policy_denied_at` BIGINT NOT NULL DEFAULT 0,
	ADD KEY `idx_meta_upstreams_policy_denied_pull_count` (`policy_denied_pull_count`),
	ADD KEY `idx_meta_upstreams_last_policy_denied_at` (`last_policy_denied_at`);
