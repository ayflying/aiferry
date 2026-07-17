-- +goose Up
ALTER TABLE `api_keys`
  ADD COLUMN `key_cipher` TEXT NULL AFTER `key_hash`;

-- +goose Down
ALTER TABLE `api_keys`
  DROP COLUMN `key_cipher`;
