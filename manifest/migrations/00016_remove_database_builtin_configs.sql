-- +goose Up
DELETE FROM `channel_types`
WHERE `built_in` = 1
  AND `code` IN ('openai', 'sub2api', 'deepseek', 'volcengine_ark', 'minimax', 'ollama', 'xiaomi_mimo', 'newapi_ratio_metadata');

-- +goose Down
SELECT 1;
