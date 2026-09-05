-- +goose Up
ALTER TABLE `usage_logs`
  ADD COLUMN `attempt_flow_json` JSON NULL COMMENT '调用重试流程（渠道与每步耗时）' AFTER `attempts`;

-- +goose Down
ALTER TABLE `usage_logs`
  DROP COLUMN `attempt_flow_json`;
