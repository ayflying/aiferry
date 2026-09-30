-- +goose Up
CREATE TABLE `model_metadata` (
  `channel_id` BIGINT UNSIGNED NOT NULL COMMENT '0=公开模型手动覆盖，正数=渠道上游元数据',
  `model_name` VARCHAR(191) COLLATE utf8mb4_bin NOT NULL COMMENT '上游名称或公开名称（channel_id=0）',
  `metadata` JSON NOT NULL COMMENT '显式元数据；null字段表示未知或继承',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`channel_id`, `model_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='模型元数据及独立手动覆盖';

-- +goose Down
DROP TABLE `model_metadata`;
