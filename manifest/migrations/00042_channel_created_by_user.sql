-- +goose Up
ALTER TABLE `channels`
  ADD COLUMN `created_by_user_id` BIGINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '创建者用户 id；存量渠道回填为管理员（1）' AFTER `id`;

-- +goose Down
ALTER TABLE `channels`
  DROP COLUMN `created_by_user_id`;
