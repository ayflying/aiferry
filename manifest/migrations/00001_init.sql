-- +goose Up
CREATE TABLE `users` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name` VARCHAR(64) NOT NULL,
  `role` VARCHAR(32) NOT NULL DEFAULT 'admin',
  `status` TINYINT NOT NULL DEFAULT 1,
  `identity_provider` VARCHAR(64) NULL,
  `identity_subject` VARCHAR(191) NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_users_identity` (`identity_provider`, `identity_subject`),
  KEY `idx_users_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `users` (`id`, `name`, `role`, `status`) VALUES (1, 'Administrator', 'admin', 1);

CREATE TABLE `api_keys` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `user_id` BIGINT UNSIGNED NOT NULL,
  `name` VARCHAR(96) NOT NULL,
  `key_prefix` VARCHAR(20) NOT NULL,
  `key_hash` CHAR(64) NOT NULL,
  `status` TINYINT NOT NULL DEFAULT 1,
  `expires_at` DATETIME(3) NULL,
  `last_used_at` DATETIME(3) NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_api_keys_hash` (`key_hash`),
  KEY `idx_api_keys_user_status` (`user_id`, `status`),
  KEY `idx_api_keys_deleted_at` (`deleted_at`),
  CONSTRAINT `fk_api_keys_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `channels` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name` VARCHAR(96) NOT NULL,
  `type` VARCHAR(32) NOT NULL DEFAULT 'openai',
  `base_url` VARCHAR(512) NOT NULL,
  `api_key_cipher` TEXT NOT NULL,
  `management_key_cipher` TEXT NULL,
  `organization_id` VARCHAR(128) NULL,
  `project_id` VARCHAR(128) NULL,
  `status` TINYINT NOT NULL DEFAULT 1,
  `priority` INT NOT NULL DEFAULT 0,
  `weight` INT UNSIGNED NOT NULL DEFAULT 1,
  `cost_query_mode` VARCHAR(32) NOT NULL DEFAULT 'none',
  `cost_query_config` JSON NULL,
  `last_test_status` VARCHAR(32) NULL,
  `last_test_latency_ms` INT UNSIGNED NULL,
  `last_test_error` VARCHAR(1024) NULL,
  `last_test_at` DATETIME(3) NULL,
  `last_cost_used` DECIMAL(20,8) NULL,
  `last_cost_remaining` DECIMAL(20,8) NULL,
  `last_cost_currency` VARCHAR(12) NULL,
  `last_cost_at` DATETIME(3) NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  KEY `idx_channels_route` (`status`, `priority`),
  KEY `idx_channels_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `channel_models` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `channel_id` BIGINT UNSIGNED NOT NULL,
  `public_name` VARCHAR(191) NOT NULL,
  `upstream_name` VARCHAR(191) NOT NULL,
  `discovered` TINYINT NOT NULL DEFAULT 1,
  `enabled` TINYINT NOT NULL DEFAULT 0,
  `input_price` DECIMAL(20,8) NULL,
  `cached_input_price` DECIMAL(20,8) NULL,
  `output_price` DECIMAL(20,8) NULL,
  `last_test_endpoint` VARCHAR(32) NULL,
  `last_test_status` VARCHAR(32) NULL,
  `last_test_latency_ms` INT UNSIGNED NULL,
  `last_test_error` VARCHAR(1024) NULL,
  `last_test_at` DATETIME(3) NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_channel_models_upstream` (`channel_id`, `upstream_name`),
  KEY `idx_channel_models_public_route` (`public_name`, `enabled`),
  KEY `idx_channel_models_deleted_at` (`deleted_at`),
  CONSTRAINT `fk_channel_models_channel` FOREIGN KEY (`channel_id`) REFERENCES `channels` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `usage_logs` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `request_id` VARCHAR(64) NOT NULL,
  `user_id` BIGINT UNSIGNED NOT NULL,
  `api_key_id` BIGINT UNSIGNED NOT NULL,
  `channel_id` BIGINT UNSIGNED NULL,
  `endpoint` VARCHAR(64) NOT NULL,
  `requested_model` VARCHAR(191) NOT NULL,
  `upstream_model` VARCHAR(191) NULL,
  `http_status` SMALLINT UNSIGNED NOT NULL,
  `is_stream` TINYINT NOT NULL DEFAULT 0,
  `input_tokens` BIGINT UNSIGNED NULL,
  `cached_input_tokens` BIGINT UNSIGNED NULL,
  `output_tokens` BIGINT UNSIGNED NULL,
  `total_tokens` BIGINT UNSIGNED NULL,
  `estimated_cost` DECIMAL(20,8) NULL,
  `duration_ms` BIGINT UNSIGNED NOT NULL,
  `first_token_ms` BIGINT UNSIGNED NULL,
  `attempts` TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `error_message` VARCHAR(1024) NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_usage_logs_request` (`request_id`),
  KEY `idx_usage_logs_created` (`created_at`, `id`),
  KEY `idx_usage_logs_dimensions` (`api_key_id`, `channel_id`, `requested_model`),
  CONSTRAINT `fk_usage_logs_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`),
  CONSTRAINT `fk_usage_logs_api_key` FOREIGN KEY (`api_key_id`) REFERENCES `api_keys` (`id`),
  CONSTRAINT `fk_usage_logs_channel` FOREIGN KEY (`channel_id`) REFERENCES `channels` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `channel_cost_snapshots` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `channel_id` BIGINT UNSIGNED NOT NULL,
  `mode` VARCHAR(32) NOT NULL,
  `used_amount` DECIMAL(20,8) NULL,
  `remaining_amount` DECIMAL(20,8) NULL,
  `currency` VARCHAR(12) NOT NULL DEFAULT 'USD',
  `period_start` DATETIME(3) NULL,
  `period_end` DATETIME(3) NULL,
  `queried_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  KEY `idx_cost_snapshots_channel_time` (`channel_id`, `queried_at`),
  CONSTRAINT `fk_cost_snapshots_channel` FOREIGN KEY (`channel_id`) REFERENCES `channels` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE IF EXISTS `channel_cost_snapshots`;
DROP TABLE IF EXISTS `usage_logs`;
DROP TABLE IF EXISTS `channel_models`;
DROP TABLE IF EXISTS `channels`;
DROP TABLE IF EXISTS `api_keys`;
DROP TABLE IF EXISTS `users`;
