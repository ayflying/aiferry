-- +goose Up
CREATE TABLE `channel_types` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name` VARCHAR(96) NOT NULL,
  `code` VARCHAR(64) NOT NULL,
  `config_json` JSON NOT NULL,
  `status` TINYINT NOT NULL DEFAULT 1,
  `built_in` TINYINT NOT NULL DEFAULT 0,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_channel_types_code` (`code`),
  KEY `idx_channel_types_status` (`status`),
  KEY `idx_channel_types_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

UPDATE `channels` SET `type` = 'sub2api' WHERE `cost_query_mode` = 'sub2api_usage';
UPDATE `channels` SET `type` = 'openai' WHERE `type` IS NULL OR `type` = '';

-- +goose Down
UPDATE `channels` SET `type` = 'openai' WHERE `type` = 'sub2api';
DROP TABLE IF EXISTS `channel_types`;
