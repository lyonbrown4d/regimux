CREATE TABLE IF NOT EXISTS `meta_prefetch_controls` (
	`id` BIGINT NOT NULL,
	`action` VARCHAR(32) NOT NULL,
	`reason` TEXT NOT NULL,
	`requested_at` BIGINT NOT NULL,
	`consumed_at` BIGINT NOT NULL,
	`created_at` BIGINT NOT NULL,
	`updated_at` BIGINT NOT NULL,
	PRIMARY KEY (`id`),
	KEY `idx_meta_prefetch_controls_action` (`action`),
	KEY `idx_meta_prefetch_controls_requested_at` (`requested_at`),
	KEY `idx_meta_prefetch_controls_consumed_at` (`consumed_at`),
	KEY `idx_meta_prefetch_controls_created_at` (`created_at`),
	KEY `idx_meta_prefetch_controls_updated_at` (`updated_at`)
)
