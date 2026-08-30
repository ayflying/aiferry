-- +goose Up
CREATE TABLE `user_channel_groups` (
  `user_id` BIGINT UNSIGNED NOT NULL,
  `channel_group_id` BIGINT UNSIGNED NOT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`user_id`, `channel_group_id`),
  KEY `idx_user_channel_groups_group` (`channel_group_id`),
  CONSTRAINT `fk_user_channel_groups_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`),
  CONSTRAINT `fk_user_channel_groups_group` FOREIGN KEY (`channel_group_id`) REFERENCES `channel_groups` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE IF EXISTS `user_channel_groups`;