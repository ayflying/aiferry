-- +goose Up
ALTER TABLE `users`
  ADD COLUMN `email` VARCHAR(320) NULL AFTER `name`,
  ADD COLUMN `balance` DECIMAL(20,8) NOT NULL DEFAULT 0 AFTER `status`,
  ADD UNIQUE KEY `uk_users_email` (`email`);

-- +goose Down
ALTER TABLE `users`
  DROP INDEX `uk_users_email`,
  DROP COLUMN `balance`,
  DROP COLUMN `email`;
