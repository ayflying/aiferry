-- +goose Up
ALTER TABLE `channels`
  ADD COLUMN `advanced_config` JSON NULL AFTER `cost_query_config`,
  ADD COLUMN `proxy_url_cipher` TEXT NULL AFTER `advanced_config`;

-- +goose Down
ALTER TABLE `channels`
  DROP COLUMN `proxy_url_cipher`,
  DROP COLUMN `advanced_config`;
