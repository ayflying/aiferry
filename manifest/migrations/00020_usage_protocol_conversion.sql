-- +goose Up
ALTER TABLE `usage_logs`
  ADD COLUMN `upstream_endpoint` VARCHAR(64) NULL AFTER `endpoint`,
  ADD COLUMN `protocol_conversion` VARCHAR(64) NULL AFTER `upstream_endpoint`,
  ADD KEY `idx_usage_logs_protocol_conversion` (`protocol_conversion`);

-- +goose Down
ALTER TABLE `usage_logs`
  DROP KEY `idx_usage_logs_protocol_conversion`,
  DROP COLUMN `protocol_conversion`,
  DROP COLUMN `upstream_endpoint`;
