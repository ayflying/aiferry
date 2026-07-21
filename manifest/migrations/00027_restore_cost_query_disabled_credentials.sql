-- +goose Up
-- Cost queries are informational and can return incomplete or unreliable
-- balances. Restore only credentials disabled by the old cost-query rule.
UPDATE `channel_credentials`
SET
  `status` = 1,
  `auto_disabled_at` = NULL,
  `auto_disabled_reason` = NULL,
  `auto_disabled_status_code` = NULL,
  `auto_disabled_source` = NULL
WHERE `status` = 0
  AND `auto_disabled_at` IS NOT NULL
  AND `auto_disabled_source` = 'cost_query';

-- +goose Down
SELECT 1;
