-- +goose Up
ALTER TABLE `usage_logs`
  ADD COLUMN `billing_details_json` TEXT NULL AFTER `estimated_cost`;

-- +goose Down
ALTER TABLE `usage_logs`
  DROP COLUMN `billing_details_json`;
