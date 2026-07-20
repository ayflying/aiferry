-- +goose Up
ALTER TABLE `api_keys`
  ADD COLUMN `daily_spend_limit` DECIMAL(20,8) NULL AFTER `spend_limit`,
  ADD COLUMN `daily_spent_amount` DECIMAL(20,8) NOT NULL DEFAULT 0 AFTER `spent_amount`,
  ADD COLUMN `daily_spend_date` DATE NULL AFTER `daily_spent_amount`;

-- +goose Down
ALTER TABLE `api_keys`
  DROP COLUMN `daily_spend_date`,
  DROP COLUMN `daily_spent_amount`,
  DROP COLUMN `daily_spend_limit`;
