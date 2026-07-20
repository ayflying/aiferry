-- +goose Up
ALTER TABLE `usage_logs`
  ADD COLUMN `client_ip` VARCHAR(45) NULL AFTER `protocol_conversion`,
  ADD COLUMN `ip_location` VARCHAR(255) NULL AFTER `client_ip`;

-- +goose Down
ALTER TABLE `usage_logs`
  DROP COLUMN `ip_location`,
  DROP COLUMN `client_ip`;
