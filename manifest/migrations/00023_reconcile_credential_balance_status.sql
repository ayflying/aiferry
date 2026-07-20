-- +goose Up
-- Existing snapshots predate balance-driven availability. Correct only enabled
-- zero-balance credentials and automatically disabled credentials with balance.
UPDATE `channel_credentials`
SET
  `status` = 0,
  `auto_disabled_at` = NOW(),
  `auto_disabled_reason` = '费用查询返回余额为 0',
  `auto_disabled_status_code` = NULL,
  `auto_disabled_source` = 'cost_query'
WHERE `status` = 1
  AND `last_cost_remaining` IS NOT NULL
  AND `last_cost_remaining` <= 0;

UPDATE `channel_credentials`
SET
  `status` = 1,
  `auto_disabled_at` = NULL,
  `auto_disabled_reason` = NULL,
  `auto_disabled_status_code` = NULL,
  `auto_disabled_source` = NULL
WHERE `status` = 0
  AND `auto_disabled_at` IS NOT NULL
  AND `last_cost_remaining` IS NOT NULL
  AND `last_cost_remaining` > 0;

-- +goose Down
SELECT 1;
