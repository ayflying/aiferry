-- +goose Up
-- JiAPI 内置渠道类型已移除：将存量 jiapi 渠道回退为 sub2api 类型。
-- 两者共用 sub2api_usage 费用适配器，回退不影响费用/余额查询。
UPDATE `channels`
SET `type` = 'sub2api'
WHERE `type` = 'jiapi';

-- +goose Down
UPDATE `channels`
SET `type` = 'jiapi'
WHERE `type` = 'sub2api'
  AND TRIM(TRAILING '/' FROM `base_url`) = 'https://api.jiapi.com/v1';
