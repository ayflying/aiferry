-- +goose Up
ALTER TABLE `usage_logs`
  MODIFY COLUMN `api_key_id` BIGINT UNSIGNED NULL;

-- +goose Down
DELETE FROM `usage_logs`
WHERE `api_key_id` IS NULL AND `request_id` LIKE 'aftest_%';

ALTER TABLE `usage_logs`
  MODIFY COLUMN `api_key_id` BIGINT UNSIGNED NOT NULL;
