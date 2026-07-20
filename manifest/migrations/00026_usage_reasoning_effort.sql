-- +goose Up
ALTER TABLE `usage_logs`
  ADD COLUMN `reasoning_effort` VARCHAR(32) NULL AFTER `upstream_model`;

-- +goose Down
ALTER TABLE `usage_logs`
  DROP COLUMN `reasoning_effort`;
