-- +goose Up
CREATE TABLE `channel_groups` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name` VARCHAR(96) NOT NULL,
  `code` VARCHAR(64) NOT NULL,
  `description` VARCHAR(255) NULL,
  `status` TINYINT NOT NULL DEFAULT 1,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_channel_groups_code` (`code`),
  KEY `idx_channel_groups_status` (`status`),
  KEY `idx_channel_groups_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `channel_group_members` (
  `channel_group_id` BIGINT UNSIGNED NOT NULL,
  `channel_id` BIGINT UNSIGNED NOT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`channel_group_id`, `channel_id`),
  KEY `idx_channel_group_members_channel` (`channel_id`),
  CONSTRAINT `fk_channel_group_members_group` FOREIGN KEY (`channel_group_id`) REFERENCES `channel_groups` (`id`),
  CONSTRAINT `fk_channel_group_members_channel` FOREIGN KEY (`channel_id`) REFERENCES `channels` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE `api_keys`
  ADD COLUMN `spend_limit` DECIMAL(20,8) NULL AFTER `status`,
  ADD COLUMN `spent_amount` DECIMAL(20,8) NOT NULL DEFAULT 0 AFTER `spend_limit`;

CREATE TABLE `api_key_models` (
  `api_key_id` BIGINT UNSIGNED NOT NULL,
  `model_name` VARCHAR(191) NOT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`api_key_id`, `model_name`),
  CONSTRAINT `fk_api_key_models_key` FOREIGN KEY (`api_key_id`) REFERENCES `api_keys` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `api_key_channel_groups` (
  `api_key_id` BIGINT UNSIGNED NOT NULL,
  `channel_group_id` BIGINT UNSIGNED NOT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`api_key_id`, `channel_group_id`),
  KEY `idx_api_key_channel_groups_group` (`channel_group_id`),
  CONSTRAINT `fk_api_key_channel_groups_key` FOREIGN KEY (`api_key_id`) REFERENCES `api_keys` (`id`),
  CONSTRAINT `fk_api_key_channel_groups_group` FOREIGN KEY (`channel_group_id`) REFERENCES `channel_groups` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `model_price_rules` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `channel_model_id` BIGINT UNSIGNED NOT NULL,
  `name` VARCHAR(96) NOT NULL,
  `source` VARCHAR(16) NOT NULL DEFAULT 'manual',
  `source_ref` VARCHAR(512) NULL,
  `priority` INT NOT NULL DEFAULT 0,
  `currency` VARCHAR(12) NOT NULL DEFAULT 'USD',
  `conditions_json` JSON NOT NULL,
  `rates_json` JSON NOT NULL,
  `status` TINYINT NOT NULL DEFAULT 1,
  `synced_at` DATETIME(3) NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  KEY `idx_model_price_rules_lookup` (`channel_model_id`, `status`, `priority`),
  KEY `idx_model_price_rules_deleted_at` (`deleted_at`),
  CONSTRAINT `fk_model_price_rules_channel_model` FOREIGN KEY (`channel_model_id`) REFERENCES `channel_models` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE IF EXISTS `model_price_rules`;
DROP TABLE IF EXISTS `api_key_channel_groups`;
DROP TABLE IF EXISTS `api_key_models`;
ALTER TABLE `api_keys` DROP COLUMN `spent_amount`, DROP COLUMN `spend_limit`;
DROP TABLE IF EXISTS `channel_group_members`;
DROP TABLE IF EXISTS `channel_groups`;
