-- +goose Up
INSERT IGNORE INTO `channel_types` (`name`, `code`, `config_json`, `status`, `built_in`) VALUES
  ('DeepSeek', 'deepseek', '{"models":{"method":"GET","path":"/models","listPath":"data","idPath":"id","authType":"channel_key","headerName":"Authorization","headerPrefix":"Bearer "},"costs":{"adapter":"none"},"pricing":{"adapter":"none"}}', 1, 1),
  ('火山方舟 Ark', 'volcengine_ark', '{"models":{"method":"GET","path":"/models","listPath":"data","idPath":"id","authType":"channel_key","headerName":"Authorization","headerPrefix":"Bearer "},"costs":{"adapter":"none"},"pricing":{"adapter":"none"}}', 1, 1),
  ('MiniMax', 'minimax', '{"models":{"method":"GET","path":"/models","listPath":"data","idPath":"id","authType":"channel_key","headerName":"Authorization","headerPrefix":"Bearer "},"costs":{"adapter":"none"},"pricing":{"adapter":"none"}}', 1, 1),
  ('Ollama', 'ollama', '{"models":{"method":"GET","path":"/models","listPath":"data","idPath":"id","authType":"channel_key","headerName":"Authorization","headerPrefix":"Bearer "},"costs":{"adapter":"none"},"pricing":{"adapter":"none"}}', 1, 1);

-- +goose Down
DELETE FROM `channel_types`
WHERE `built_in` = 1
  AND `code` IN ('deepseek', 'volcengine_ark', 'minimax', 'ollama');
