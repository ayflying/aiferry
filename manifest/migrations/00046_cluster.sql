-- +goose Up
-- 分布式集群：主节点维护节点注册、配置版本号与用量回传批次。
-- 这四张表只由 internal/logic/cluster 通过显式 SQL 访问（无 DAO 生成依赖），
-- 因此时间列直接带数据库默认值，不依赖 GoFrame 的 DO 自动填充。
CREATE TABLE IF NOT EXISTS `cluster_nodes` (
  `id`                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name`              VARCHAR(64)     NOT NULL COMMENT '节点名，全局唯一',
  `address`           VARCHAR(255)    NOT NULL DEFAULT '' COMMENT '子节点回调基地址，如 http://10.0.0.5:8080',
  `version`           VARCHAR(32)     NOT NULL DEFAULT '' COMMENT '节点上报的 AiFerry 版本',
  `config_version`    BIGINT          NOT NULL DEFAULT 0 COMMENT '节点最后一次成功应用的配置版本',
  `pending_rows`      BIGINT          NOT NULL DEFAULT 0 COMMENT '节点待回传用量行数',
  `last_error`        VARCHAR(512)    NOT NULL DEFAULT '' COMMENT '节点最后一次同步/回传错误摘要',
  `last_heartbeat_at` DATETIME(3)     NULL COMMENT '最后一次心跳时间',
  `last_sync_at`      DATETIME(3)     NULL COMMENT '最后一次成功拉取配置时间',
  `status`            TINYINT         NOT NULL DEFAULT 1 COMMENT '1 启用，0 已吊销',
  `created_at`        DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`        DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_cluster_nodes_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='集群子节点注册表';

CREATE TABLE IF NOT EXISTS `cluster_state` (
  `id`             TINYINT NOT NULL DEFAULT 1 COMMENT '单行表，固定为 1',
  `config_version` BIGINT  NOT NULL DEFAULT 1 COMMENT '全局配置版本号，单调递增',
  `updated_at`     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='集群全局状态（单行）';

INSERT INTO `cluster_state` (`id`, `config_version`) VALUES (1, 1)
ON DUPLICATE KEY UPDATE `id` = `id`;

CREATE TABLE IF NOT EXISTS `cluster_config_events` (
  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `kind`       VARCHAR(32)     NOT NULL DEFAULT '' COMMENT '变更类别，如 channel/credential/price/setting',
  `ref`        VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '变更对象标识，仅用于排查',
  `created_at` DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  KEY `idx_cluster_config_events_created` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='配置版本递增事件流水';

CREATE TABLE IF NOT EXISTS `cluster_usage_batches` (
  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `node_id`    BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `batch_id`   VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '子节点生成的批次号，用于幂等重放',
  `rows`       INT             NOT NULL DEFAULT 0 COMMENT '本批成功落库的用量行数',
  `created_at` DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_cluster_usage_batches` (`node_id`, `batch_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='子节点用量回传批次';

ALTER TABLE `usage_logs`
  ADD COLUMN `node_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '来源子节点 ID，0 表示主节点本地产生' AFTER `id`,
  ADD KEY `idx_usage_logs_node` (`node_id`, `created_at`);

-- +goose Down
ALTER TABLE `usage_logs`
  DROP KEY `idx_usage_logs_node`,
  DROP COLUMN `node_id`;

DROP TABLE IF EXISTS `cluster_usage_batches`;
DROP TABLE IF EXISTS `cluster_config_events`;
DROP TABLE IF EXISTS `cluster_state`;
DROP TABLE IF EXISTS `cluster_nodes`;