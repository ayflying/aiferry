-- +goose Up
ALTER TABLE `channel_models`
  ADD COLUMN `closed_windows_json` TEXT NULL COMMENT '定时关闭时段 JSON（{tz, weekdays:[1-7], ranges:[[HH:MM,HH:MM]]}），空表示不关闭' AFTER `auto_disabled_source`;

-- +goose Down
ALTER TABLE `channel_models`
  DROP COLUMN `closed_windows_json`;
