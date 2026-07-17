-- +goose Up
CREATE TABLE `price_sources` (
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
  UNIQUE KEY `uk_price_sources_code` (`code`),
  KEY `idx_price_sources_status` (`status`),
  KEY `idx_price_sources_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `price_sources` (`name`, `code`, `config_json`, `status`, `built_in`) VALUES
  ('BaseLLM 官方模型价格', 'basellm_official', '{"baseUrl":"https://basellm.github.io","pricing":{"adapter":"newapi_ratio","method":"GET","path":"/llm-metadata/api/newapi/ratio_config-v1-base.json","authType":"none"}}', 1, 1)
ON DUPLICATE KEY UPDATE
  `name` = VALUES(`name`),
  `config_json` = VALUES(`config_json`),
  `status` = VALUES(`status`),
  `built_in` = VALUES(`built_in`),
  `deleted_at` = NULL;

-- The former BaseLLM entry was a price-only pseudo channel. Preserve its
-- historical relations, but remove it from all channel and channel type views.
UPDATE `channels`
SET `status` = 0, `deleted_at` = CURRENT_TIMESTAMP(3)
WHERE `type` = 'newapi_ratio_metadata' AND `deleted_at` IS NULL;

UPDATE `channel_types`
SET `status` = 0, `deleted_at` = CURRENT_TIMESTAMP(3)
WHERE `code` = 'newapi_ratio_metadata' AND `deleted_at` IS NULL;

-- +goose Down
UPDATE `channel_types`
SET `status` = 1, `deleted_at` = NULL
WHERE `code` = 'newapi_ratio_metadata';

UPDATE `channels`
SET `status` = 1, `deleted_at` = NULL
WHERE `type` = 'newapi_ratio_metadata';

DROP TABLE IF EXISTS `price_sources`;
