-- +goose Up
ALTER TABLE `channel_models`
  ADD COLUMN `health_score` INT NOT NULL DEFAULT 100,
  ADD COLUMN `auto_disabled_at` DATETIME NULL,
  ADD COLUMN `auto_disabled_reason` VARCHAR(1024) NULL,
  ADD COLUMN `auto_disabled_source` VARCHAR(32) NULL;

-- +goose Down
ALTER TABLE `channel_models`
  DROP COLUMN `health_score`,
  DROP COLUMN `auto_disabled_at`,
  DROP COLUMN `auto_disabled_reason`,
  DROP COLUMN `auto_disabled_source`;
