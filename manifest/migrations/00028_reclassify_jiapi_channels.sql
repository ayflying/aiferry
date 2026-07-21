-- +goose Up
UPDATE `channels`
SET `type` = 'jiapi'
WHERE `type` = 'sub2api'
  AND TRIM(TRAILING '/' FROM `base_url`) = 'https://api.jiapi.com/v1';

-- +goose Down
UPDATE `channels`
SET `type` = 'sub2api'
WHERE `type` = 'jiapi'
  AND TRIM(TRAILING '/' FROM `base_url`) = 'https://api.jiapi.com/v1';
