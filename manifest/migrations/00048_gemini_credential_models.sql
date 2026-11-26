-- +goose Up
CREATE TABLE `gemini_credential_models` (
  `channel_id` BIGINT UNSIGNED NOT NULL,
  `channel_credential_id` BIGINT UNSIGNED NOT NULL,
  `upstream_name` VARCHAR(191) COLLATE utf8mb4_bin NOT NULL,
  `metadata` JSON NOT NULL,
  PRIMARY KEY (`channel_credential_id`, `upstream_name`),
  KEY `idx_gemini_credential_models_route` (`channel_id`, `upstream_name`),
  CONSTRAINT `fk_gemini_credential_models_credential` FOREIGN KEY (`channel_credential_id`) REFERENCES `channel_credentials` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Gemini 账号同步得到的模型目录，不记录请求健康状态';

-- +goose Down
DROP TABLE `gemini_credential_models`;
