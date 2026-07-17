-- +goose Up
SELECT 1;

-- +goose Down
DELETE FROM `channel_types` WHERE `code` = 'xiaomi_mimo' AND `built_in` = 1;
