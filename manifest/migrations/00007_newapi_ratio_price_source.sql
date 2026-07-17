-- +goose Up
ALTER TABLE `model_prices`
  ADD COLUMN `billing_mode` VARCHAR(16) NOT NULL DEFAULT 'token' AFTER `public_name`,
  ADD COLUMN `cache_write_price` DECIMAL(20,8) NULL AFTER `cached_input_price`,
  ADD COLUMN `image_input_price` DECIMAL(20,8) NULL AFTER `output_price`,
  ADD COLUMN `audio_input_price` DECIMAL(20,8) NULL AFTER `image_input_price`,
  ADD COLUMN `audio_output_price` DECIMAL(20,8) NULL AFTER `audio_input_price`,
  ADD COLUMN `request_price` DECIMAL(20,8) NULL AFTER `audio_output_price`;

UPDATE `model_prices` AS `price`
INNER JOIN (
  SELECT DISTINCT `model_name`
  FROM `model_price_rules`
  WHERE `source` = 'manual' AND `status` = 1 AND `deleted_at` IS NULL
) AS `rule` ON `rule`.`model_name` = `price`.`public_name`
SET `price`.`billing_mode` = 'rules';

INSERT INTO `channel_types` (`name`, `code`, `config_json`, `status`, `built_in`) VALUES
  ('BaseLLM 官方模型价格', 'newapi_ratio_metadata', '{"priceSyncOnly":true,"costs":{"adapter":"none"},"pricing":{"adapter":"newapi_ratio","method":"GET","path":"/llm-metadata/api/newapi/ratio_config-v1-base.json","authType":"none"}}', 1, 1)
ON DUPLICATE KEY UPDATE
  `name` = VALUES(`name`),
  `config_json` = VALUES(`config_json`),
  `status` = VALUES(`status`),
  `built_in` = VALUES(`built_in`),
  `deleted_at` = NULL;

INSERT INTO `channels` (`name`, `type`, `base_url`, `api_key_cipher`, `status`, `priority`, `weight`, `cost_query_mode`, `cost_query_config`)
SELECT 'BaseLLM 官方模型价格', 'newapi_ratio_metadata', 'https://basellm.github.io', '', 1, -1000, 1, 'none', '{}'
WHERE NOT EXISTS (
  SELECT 1 FROM `channels`
  WHERE `type` = 'newapi_ratio_metadata'
    AND `base_url` = 'https://basellm.github.io'
    AND `deleted_at` IS NULL
);

-- +goose Down
DELETE FROM `channels`
WHERE `type` = 'newapi_ratio_metadata'
  AND `base_url` = 'https://basellm.github.io';

DELETE FROM `channel_types` WHERE `code` = 'newapi_ratio_metadata';

ALTER TABLE `model_prices`
  DROP COLUMN `request_price`,
  DROP COLUMN `audio_output_price`,
  DROP COLUMN `audio_input_price`,
  DROP COLUMN `image_input_price`,
  DROP COLUMN `cache_write_price`,
  DROP COLUMN `billing_mode`;
