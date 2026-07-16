-- +goose Up
CREATE TABLE `model_prices` (
  `public_name` VARCHAR(191) NOT NULL,
  `input_price` DECIMAL(20,8) NULL,
  `cached_input_price` DECIMAL(20,8) NULL,
  `output_price` DECIMAL(20,8) NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at` DATETIME(3) NULL,
  PRIMARY KEY (`public_name`),
  KEY `idx_model_prices_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `model_prices` (`public_name`, `input_price`, `cached_input_price`, `output_price`, `created_at`, `updated_at`)
SELECT
  `public_name`,
  MAX(`input_price`),
  MAX(`cached_input_price`),
  MAX(`output_price`),
  MIN(`created_at`),
  MAX(`updated_at`)
FROM `channel_models`
WHERE `deleted_at` IS NULL
GROUP BY `public_name`;

ALTER TABLE `model_price_rules`
  ADD COLUMN `model_name` VARCHAR(191) NULL AFTER `channel_model_id`;

UPDATE `model_price_rules` AS `rule`
INNER JOIN `channel_models` AS `model` ON `model`.`id` = `rule`.`channel_model_id`
SET `rule`.`model_name` = `model`.`public_name`
WHERE `rule`.`model_name` IS NULL;

ALTER TABLE `model_price_rules`
  MODIFY COLUMN `model_name` VARCHAR(191) NOT NULL,
  ADD KEY `idx_model_price_rules_model` (`model_name`, `status`, `priority`);

-- +goose Down
ALTER TABLE `model_price_rules`
  DROP KEY `idx_model_price_rules_model`,
  DROP COLUMN `model_name`;

DROP TABLE IF EXISTS `model_prices`;
