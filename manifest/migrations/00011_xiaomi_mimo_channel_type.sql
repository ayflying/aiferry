-- +goose Up
INSERT IGNORE INTO `channel_types` (`name`, `code`, `config_json`, `status`, `built_in`) VALUES
  ('小米 MiMo', 'xiaomi_mimo', '{"models":{"method":"GET","path":"/models","listPath":"data","idPath":"id","authType":"channel_key","headerName":"Authorization","headerPrefix":"Bearer "},"costs":{"adapter":"none"},"pricing":{"adapter":"none"}}', 1, 1);

-- +goose Down
DELETE FROM `channel_types` WHERE `code` = 'xiaomi_mimo' AND `built_in` = 1;
