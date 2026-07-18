-- +goose Up
ALTER TABLE `channels`
  ADD COLUMN `health_check_model_id` BIGINT UNSIGNED NULL AFTER `weight`,
  ADD COLUMN `auto_disable_enabled` TINYINT NOT NULL DEFAULT 1 AFTER `health_check_model_id`,
  ADD KEY `idx_channels_health_check_model` (`health_check_model_id`),
  ADD CONSTRAINT `fk_channels_health_check_model` FOREIGN KEY (`health_check_model_id`) REFERENCES `channel_models` (`id`) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE `channels`
  DROP FOREIGN KEY `fk_channels_health_check_model`,
  DROP KEY `idx_channels_health_check_model`,
  DROP COLUMN `auto_disable_enabled`,
  DROP COLUMN `health_check_model_id`;
