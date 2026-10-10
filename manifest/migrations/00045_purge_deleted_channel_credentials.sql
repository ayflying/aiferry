-- +goose Up
-- 已删除密钥不再保留密文及唯一索引占位；历史调用日志保留，凭据引用由外键置空。
DELETE s FROM `channel_credential_cost_snapshots` s
INNER JOIN `channel_credentials` c ON c.id = s.channel_credential_id
WHERE c.deleted_at IS NOT NULL;

DELETE m FROM `channel_model_credentials` m
INNER JOIN `channel_credentials` c ON c.id = m.channel_credential_id
WHERE c.deleted_at IS NOT NULL;

DELETE FROM `channel_credentials` WHERE `deleted_at` IS NOT NULL;

-- +goose Down
-- 物理删除的密钥不可恢复；保留 deleted_at 列以兼容既有生成模型。
SELECT 1;
