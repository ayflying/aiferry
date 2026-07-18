-- +goose Up
CREATE TABLE `channel_credentials` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `channel_id` BIGINT UNSIGNED NOT NULL,
  `key_prefix` VARCHAR(32) NOT NULL DEFAULT '',
  `key_hash` CHAR(64) NOT NULL DEFAULT '',
  `api_key_cipher` TEXT NOT NULL,
  `status` TINYINT NOT NULL DEFAULT 1,
  `auto_disabled_at` DATETIME(3) NULL,
  `auto_disabled_reason` VARCHAR(1024) NULL,
  `auto_disabled_status_code` SMALLINT UNSIGNED NULL,
  `auto_disabled_source` VARCHAR(32) NULL,
  `last_cost_used` DECIMAL(20,8) NULL,
  `last_cost_remaining` DECIMAL(20,8) NULL,
  `last_cost_currency` VARCHAR(12) NULL,
  `last_cost_at` DATETIME(3) NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_channel_credentials_hash` (`channel_id`, `key_hash`),
  KEY `idx_channel_credentials_route` (`channel_id`, `status`, `id`),
  KEY `idx_channel_credentials_auto_disabled` (`channel_id`, `status`, `auto_disabled_at`),
  KEY `idx_channel_credentials_deleted_at` (`deleted_at`),
  CONSTRAINT `fk_channel_credentials_channel` FOREIGN KEY (`channel_id`) REFERENCES `channels` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `channel_credentials` (`channel_id`, `api_key_cipher`, `status`)
SELECT `id`, `api_key_cipher`, 1
FROM `channels`
WHERE `deleted_at` IS NULL
  AND `api_key_cipher` <> '';

CREATE TABLE `api_key_channel_credentials` (
  `api_key_id` BIGINT UNSIGNED NOT NULL,
  `channel_id` BIGINT UNSIGNED NOT NULL,
  `channel_credential_id` BIGINT UNSIGNED NOT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`api_key_id`, `channel_id`),
  KEY `idx_api_key_channel_credentials_credential` (`channel_credential_id`),
  CONSTRAINT `fk_api_key_channel_credentials_api_key` FOREIGN KEY (`api_key_id`) REFERENCES `api_keys` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_api_key_channel_credentials_channel` FOREIGN KEY (`channel_id`) REFERENCES `channels` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_api_key_channel_credentials_credential` FOREIGN KEY (`channel_credential_id`) REFERENCES `channel_credentials` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `channel_credential_cost_snapshots` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `channel_credential_id` BIGINT UNSIGNED NOT NULL,
  `mode` VARCHAR(32) NOT NULL,
  `used_amount` DECIMAL(20,8) NULL,
  `remaining_amount` DECIMAL(20,8) NULL,
  `currency` VARCHAR(12) NOT NULL DEFAULT 'USD',
  `period_start` DATETIME(3) NULL,
  `period_end` DATETIME(3) NULL,
  `queried_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  KEY `idx_credential_cost_snapshots_time` (`channel_credential_id`, `queried_at`),
  CONSTRAINT `fk_credential_cost_snapshots_credential` FOREIGN KEY (`channel_credential_id`) REFERENCES `channel_credentials` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE `usage_logs`
  ADD COLUMN `channel_credential_id` BIGINT UNSIGNED NULL AFTER `channel_id`,
  ADD KEY `idx_usage_logs_credential` (`channel_credential_id`),
  ADD CONSTRAINT `fk_usage_logs_credential` FOREIGN KEY (`channel_credential_id`) REFERENCES `channel_credentials` (`id`) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE `usage_logs`
  DROP FOREIGN KEY `fk_usage_logs_credential`,
  DROP KEY `idx_usage_logs_credential`,
  DROP COLUMN `channel_credential_id`;

DROP TABLE IF EXISTS `channel_credential_cost_snapshots`;
DROP TABLE IF EXISTS `api_key_channel_credentials`;
DROP TABLE IF EXISTS `channel_credentials`;
