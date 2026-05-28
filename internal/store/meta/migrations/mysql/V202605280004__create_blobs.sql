CREATE TABLE IF NOT EXISTS `meta_blobs` (
	`id` BIGINT NOT NULL,
	`digest` VARCHAR(128) NOT NULL,
	`size` BIGINT NOT NULL,
	`media_type` VARCHAR(255) NOT NULL,
	`object_key` VARCHAR(512) NOT NULL,
	`created_at` BIGINT NOT NULL,
	`updated_at` BIGINT NOT NULL,
	`last_access_at` BIGINT NOT NULL,
	PRIMARY KEY (`id`),
	UNIQUE KEY `idx_meta_blobs_digest` (`digest`),
	KEY `idx_meta_blobs_size` (`size`),
	KEY `idx_meta_blobs_created_at` (`created_at`),
	KEY `idx_meta_blobs_updated_at` (`updated_at`),
	KEY `idx_meta_blobs_last_access_at` (`last_access_at`)
)
