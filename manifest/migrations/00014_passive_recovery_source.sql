-- +goose Up
ALTER TABLE `channels`
  ADD COLUMN `auto_disabled_source` VARCHAR(32) NULL AFTER `auto_disabled_status_code`,
  ADD KEY `idx_channels_auto_disabled_source` (`status`, `auto_disabled_source`, `auto_disabled_at`);

UPDATE `channels`
SET `auto_disabled_source` = 'legacy'
WHERE `auto_disabled_at` IS NOT NULL
  AND `auto_disabled_source` IS NULL;

-- +goose Down
ALTER TABLE `channels`
  DROP KEY `idx_channels_auto_disabled_source`,
  DROP COLUMN `auto_disabled_source`;
