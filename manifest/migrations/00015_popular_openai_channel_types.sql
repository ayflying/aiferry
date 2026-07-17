-- +goose Up
SELECT 1;

-- +goose Down
DELETE FROM `channel_types`
WHERE `built_in` = 1
  AND `code` IN ('deepseek', 'volcengine_ark', 'minimax', 'ollama');
