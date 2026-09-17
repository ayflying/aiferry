-- +goose Up
CREATE TABLE `channel_model_credentials` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `channel_id` BIGINT UNSIGNED NOT NULL,
  `channel_model_id` BIGINT UNSIGNED NOT NULL,
  `channel_credential_id` BIGINT UNSIGNED NOT NULL,
  `health_score` INT NOT NULL DEFAULT 100 COMMENT '组合健康分（0-100），默认100',
  `cooldown_until` DATETIME(3) NULL COMMENT '冷却截至时间，NULL 表示未冷却',
  `last_error` VARCHAR(1024) NULL COMMENT '最近一次错误摘要',
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_channel_model_credentials_model_credential` (`channel_model_id`, `channel_credential_id`),
  KEY `idx_channel_model_credentials_channel` (`channel_id`),
  CONSTRAINT `fk_channel_model_credentials_channel` FOREIGN KEY (`channel_id`) REFERENCES `channels` (`id`),
  CONSTRAINT `fk_channel_model_credentials_model` FOREIGN KEY (`channel_model_id`) REFERENCES `channel_models` (`id`),
  CONSTRAINT `fk_channel_model_credentials_credential` FOREIGN KEY (`channel_credential_id`) REFERENCES `channel_credentials` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='渠道 × 模型 × 凭证 组合健康分';

-- 为有效（已启用且未删除）的模型与有效（启用且未删除）的凭证初始化组合。
-- 继承旧模型健康分；旧模型健康分为 0 的组合额外进入 5 分钟冷却。
INSERT INTO `channel_model_credentials`
  (`channel_id`, `channel_model_id`, `channel_credential_id`, `health_score`, `cooldown_until`, `created_at`, `updated_at`)
SELECT
  cm.`channel_id`,
  cm.`id`,
  cc.`id`,
  cm.`health_score`,
  CASE WHEN COALESCE(cm.`health_score`, 100) = 0
       THEN DATE_ADD(NOW(3), INTERVAL 5 MINUTE)
       ELSE NULL END,
  NOW(3),
  NOW(3)
FROM `channel_models` cm
INNER JOIN `channel_credentials` cc ON cc.`channel_id` = cm.`channel_id`
WHERE cm.`deleted_at` IS NULL
  AND cm.`enabled` = 1
  AND cc.`deleted_at` IS NULL
  AND cc.`status` = 1;

-- 旧模型禁用记录迁移为组合冷却；只清自动模型标记，不更改渠道/凭证人工开关。
UPDATE channel_models cm
SET cm.auto_disabled_at = NULL, cm.auto_disabled_reason = NULL, cm.auto_disabled_source = NULL
WHERE cm.deleted_at IS NULL AND cm.enabled = 1
AND EXISTS (SELECT 1 FROM channel_model_credentials h WHERE h.channel_model_id = cm.id);

-- +goose Down
DROP TABLE IF EXISTS `channel_model_credentials`;
