-- +goose Up
ALTER TABLE `users`
  ADD COLUMN `avatar_url` VARCHAR(512) NULL AFTER `identity_subject`,
  ADD COLUMN `groups_json` JSON NULL AFTER `avatar_url`,
  ADD COLUMN `last_login_at` DATETIME(3) NULL AFTER `groups_json`;

-- +goose Down
ALTER TABLE `users`
  DROP COLUMN `last_login_at`,
  DROP COLUMN `groups_json`,
  DROP COLUMN `avatar_url`;
