-- +goose Up
CREATE TABLE `user_admin_overrides` (
  `user_id` BIGINT UNSIGNED NOT NULL,
  `original_role` VARCHAR(32) NOT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`user_id`),
  CONSTRAINT `fk_user_admin_overrides_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='AiFerry 本地管理员授权覆盖';

CREATE TABLE `user_admin_role_lock` (
  `id` TINYINT UNSIGNED NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='管理员角色变更互斥锁';
INSERT INTO `user_admin_role_lock` (`id`) VALUES (1);

-- +goose Down
DROP TABLE `user_admin_role_lock`;
DROP TABLE `user_admin_overrides`;
