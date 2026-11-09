-- +goose Up
ALTER TABLE `channel_credentials`
  ADD COLUMN `remark` VARCHAR(500) NOT NULL DEFAULT '' COMMENT '渠道密钥备注，仅管理端展示，不参与转发与计费' AFTER `management_key_cipher`;

-- +goose Down
ALTER TABLE `channel_credentials`
  DROP COLUMN `remark`;
