-- +goose Up
ALTER TABLE `channel_models`
  DROP INDEX `uk_channel_models_upstream`;

-- +goose Down
ALTER TABLE `channel_models`
  ADD UNIQUE KEY `uk_channel_models_upstream` (`channel_id`, `upstream_name`);
