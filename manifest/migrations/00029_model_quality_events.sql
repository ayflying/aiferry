-- +goose Up
CREATE TABLE `model_quality_events` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `request_id` VARCHAR(64) NOT NULL,
  `channel_id` BIGINT UNSIGNED NOT NULL,
  `credential_id` BIGINT UNSIGNED NOT NULL,
  `requested_model` VARCHAR(191) NOT NULL,
  `expected_model` VARCHAR(191) NOT NULL,
  `observed_model` VARCHAR(191) NOT NULL DEFAULT '',
  `reasons_json` JSON NOT NULL,
  `question_chars` INT UNSIGNED NOT NULL,
  `answer_chars` INT UNSIGNED NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_model_quality_events_created_at` (`created_at`),
  KEY `idx_model_quality_events_channel_id` (`channel_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE `model_quality_events`;
