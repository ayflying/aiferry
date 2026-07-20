-- +goose Up
CREATE TABLE `redemption_codes` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name` VARCHAR(20) NOT NULL,
  `code` VARCHAR(64) NOT NULL,
  `amount` DECIMAL(20,8) NOT NULL,
  `expires_at` DATETIME(3) NULL,
  `redeemed_by_user_id` BIGINT UNSIGNED NULL,
  `redeemed_at` DATETIME(3) NULL,
  `created_by_user_id` BIGINT UNSIGNED NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_redemption_codes_code` (`code`),
  KEY `idx_redemption_codes_state` (`redeemed_at`, `expires_at`, `created_at`),
  KEY `idx_redemption_codes_redeemer` (`redeemed_by_user_id`),
  CONSTRAINT `fk_redemption_codes_redeemer` FOREIGN KEY (`redeemed_by_user_id`) REFERENCES `users` (`id`) ON DELETE SET NULL,
  CONSTRAINT `fk_redemption_codes_creator` FOREIGN KEY (`created_by_user_id`) REFERENCES `users` (`id`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE IF EXISTS `redemption_codes`;
