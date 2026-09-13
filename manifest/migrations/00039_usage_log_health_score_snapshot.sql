-- +goose Up
ALTER TABLE `usage_logs`
  ADD COLUMN `health_score_at_request` INT NULL COMMENT '请求时模型健康分快照' AFTER `reasoning_effort`;

-- +goose Down
ALTER TABLE `usage_logs`
  DROP COLUMN `health_score_at_request`;
