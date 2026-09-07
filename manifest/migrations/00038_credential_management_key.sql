-- +goose Up
ALTER TABLE `channel_credentials`
  ADD COLUMN `management_key_cipher` TEXT NULL COMMENT '凭证级管理密钥密文（优先于渠道级，用于按上游账号查询用量）' AFTER `api_key_cipher`;

-- +goose Down
ALTER TABLE `channel_credentials`
  DROP COLUMN `management_key_cipher`;
