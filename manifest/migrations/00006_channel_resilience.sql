-- +goose Up
ALTER TABLE `channels`
  ADD COLUMN `auto_disabled_at` DATETIME(3) NULL AFTER `status`,
  ADD COLUMN `auto_disabled_reason` VARCHAR(1024) NULL AFTER `auto_disabled_at`,
  ADD COLUMN `auto_disabled_status_code` SMALLINT UNSIGNED NULL AFTER `auto_disabled_reason`,
  ADD KEY `idx_channels_auto_disabled` (`status`, `auto_disabled_at`);

CREATE TABLE `system_settings` (
  `setting_key` VARCHAR(64) NOT NULL,
  `value_json` JSON NOT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`setting_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `system_settings` (`setting_key`, `value_json`) VALUES
  ('channel_resilience', JSON_OBJECT(
    'maxFailoverAttempts', 3,
    'retryStatusCodes', '401,403,404,408,429,500-599',
    'healthCheckEnabled', FALSE,
    'healthCheckMode', 'passive',
    'healthCheckIntervalMinutes', 5,
    'recoveryEnabled', TRUE,
    'autoDisableEnabled', TRUE,
    'disableLatencySeconds', 120,
    'disableStatusCodes', '401,429',
    'failureKeywords', JSON_ARRAY(
      'Your credit balance is too low',
      'This organization has been disabled.',
      'You exceeded your current quota',
      'Permission denied',
      'The security token included in the request is invalid',
      'Operation not allowed',
      'Your account is not authorized',
      'daily usage limit exceeded',
      'Insufficient account balance'
    )
  ));

-- +goose Down
DROP TABLE IF EXISTS `system_settings`;

ALTER TABLE `channels`
  DROP KEY `idx_channels_auto_disabled`,
  DROP COLUMN `auto_disabled_status_code`,
  DROP COLUMN `auto_disabled_reason`,
  DROP COLUMN `auto_disabled_at`;
