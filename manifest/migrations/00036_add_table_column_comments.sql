-- +goose Up
-- 为全部表和字段补充中文备注，便于运维排查与后续开发理解表结构语义。
-- MODIFY COLUMN 完整保留原有类型、默认值与位置属性，仅追加 COMMENT。

-- ==================== 用户与密钥 ====================
ALTER TABLE `users`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '用户ID',
  MODIFY COLUMN `name` VARCHAR(64) NOT NULL COMMENT '用户昵称',
  MODIFY COLUMN `email` VARCHAR(320) NULL COMMENT '邮箱（唯一，允许为空）',
  MODIFY COLUMN `role` VARCHAR(32) NOT NULL DEFAULT 'admin' COMMENT '角色（admin=管理员，其他为普通用户）',
  MODIFY COLUMN `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态（1=启用，0=禁用）',
  MODIFY COLUMN `balance` DECIMAL(20,8) NOT NULL DEFAULT 0 COMMENT '账户余额',
  MODIFY COLUMN `identity_provider` VARCHAR(64) NULL COMMENT '外部身份提供方（casdoor）',
  MODIFY COLUMN `identity_subject` VARCHAR(191) NULL COMMENT '外部身份唯一标识（Casdoor uid）',
  MODIFY COLUMN `avatar_url` VARCHAR(512) NULL COMMENT '头像地址',
  MODIFY COLUMN `groups_json` JSON NULL COMMENT '外部用户组（遗留字段，已由 user_channel_groups 取代）',
  MODIFY COLUMN `last_login_at` DATETIME(3) NULL COMMENT '最后登录时间',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  MODIFY COLUMN `deleted_at` DATETIME(3) NULL COMMENT '软删除时间',
  COMMENT='用户表：Casdoor 登录用户的本地镜像，保存余额、角色等业务数据';

ALTER TABLE `api_keys`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '密钥ID',
  MODIFY COLUMN `user_id` BIGINT UNSIGNED NOT NULL COMMENT '所属用户ID',
  MODIFY COLUMN `name` VARCHAR(96) NOT NULL COMMENT '密钥名称',
  MODIFY COLUMN `key_prefix` VARCHAR(20) NOT NULL COMMENT '密钥前缀（用于列表展示）',
  MODIFY COLUMN `key_hash` CHAR(64) NOT NULL COMMENT '密钥 SHA-256 哈希（用于鉴权查找）',
  MODIFY COLUMN `key_cipher` TEXT NULL COMMENT '密钥加密全文（用于重新展示）',
  MODIFY COLUMN `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态（1=启用，0=禁用）',
  MODIFY COLUMN `spend_limit` DECIMAL(20,8) NULL COMMENT '累计消费上限（NULL=不限）',
  MODIFY COLUMN `spent_amount` DECIMAL(20,8) NOT NULL DEFAULT 0 COMMENT '累计已消费金额',
  MODIFY COLUMN `daily_spend_limit` DECIMAL(20,8) NULL COMMENT '单日消费上限（NULL=不限）',
  MODIFY COLUMN `daily_spent_amount` DECIMAL(20,8) NOT NULL DEFAULT 0 COMMENT '当日已消费金额',
  MODIFY COLUMN `daily_spend_date` DATE NULL COMMENT '当日消费统计归属日期',
  MODIFY COLUMN `expires_at` DATETIME(3) NULL COMMENT '过期时间（NULL=永不过期）',
  MODIFY COLUMN `last_used_at` DATETIME(3) NULL COMMENT '最后使用时间',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  MODIFY COLUMN `deleted_at` DATETIME(3) NULL COMMENT '软删除时间',
  COMMENT='API 密钥表：网关调用凭证，含累计/单日消费限额';

ALTER TABLE `user_channel_groups`
  MODIFY COLUMN `user_id` BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
  MODIFY COLUMN `channel_group_id` BIGINT UNSIGNED NOT NULL COMMENT '渠道组ID',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  COMMENT='用户渠道组授权表：用户可访问的渠道组（空=不限制）';

ALTER TABLE `redemption_codes`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '兑换码ID',
  MODIFY COLUMN `name` VARCHAR(20) NOT NULL COMMENT '兑换码名称',
  MODIFY COLUMN `code` VARCHAR(64) NOT NULL COMMENT '兑换码内容（唯一）',
  MODIFY COLUMN `amount` DECIMAL(20,8) NOT NULL COMMENT '兑换金额',
  MODIFY COLUMN `expires_at` DATETIME(3) NULL COMMENT '过期时间（NULL=永不过期）',
  MODIFY COLUMN `redeemed_by_user_id` BIGINT UNSIGNED NULL COMMENT '兑换用户ID',
  MODIFY COLUMN `redeemed_at` DATETIME(3) NULL COMMENT '兑换时间',
  MODIFY COLUMN `created_by_user_id` BIGINT UNSIGNED NULL COMMENT '创建人用户ID',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  COMMENT='兑换码表：余额充值兑换码';

-- ==================== 渠道与凭证 ====================
ALTER TABLE `channels`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '渠道ID',
  MODIFY COLUMN `name` VARCHAR(96) NOT NULL COMMENT '渠道名称',
  MODIFY COLUMN `type` VARCHAR(32) NOT NULL DEFAULT 'openai' COMMENT '渠道类型代码（对应 channel_types.code）',
  MODIFY COLUMN `base_url` VARCHAR(512) NOT NULL COMMENT '上游基础地址',
  MODIFY COLUMN `api_key_cipher` TEXT NOT NULL COMMENT '默认上游密钥密文（遗留，新逻辑用 channel_credentials）',
  MODIFY COLUMN `management_key_cipher` TEXT NULL COMMENT '管理密钥密文（费用查询等管理接口）',
  MODIFY COLUMN `organization_id` VARCHAR(128) NULL COMMENT 'OpenAI-Organization 请求头',
  MODIFY COLUMN `project_id` VARCHAR(128) NULL COMMENT 'OpenAI-Project 请求头',
  MODIFY COLUMN `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态（1=启用，0=禁用）',
  MODIFY COLUMN `auto_disabled_at` DATETIME(3) NULL COMMENT '自动禁用时间',
  MODIFY COLUMN `auto_disabled_reason` VARCHAR(1024) NULL COMMENT '自动禁用原因',
  MODIFY COLUMN `auto_disabled_status_code` SMALLINT UNSIGNED NULL COMMENT '触发自动禁用的上游状态码',
  MODIFY COLUMN `auto_disabled_source` VARCHAR(32) NULL COMMENT '自动禁用来源（relay/cost_query/legacy 等）',
  MODIFY COLUMN `priority` INT NOT NULL DEFAULT 0 COMMENT '路由优先级（数值越大越优先）',
  MODIFY COLUMN `weight` INT UNSIGNED NOT NULL DEFAULT 1 COMMENT '同优先级内的加权随机权重',
  MODIFY COLUMN `health_check_model_id` BIGINT UNSIGNED NULL COMMENT '健康检查使用的模型ID',
  MODIFY COLUMN `auto_disable_enabled` TINYINT NOT NULL DEFAULT 1 COMMENT '是否允许自动禁用（1=允许）',
  MODIFY COLUMN `cost_query_mode` VARCHAR(32) NOT NULL DEFAULT 'none' COMMENT '费用查询模式（none/openai_costs/sub2api_usage 等）',
  MODIFY COLUMN `cost_query_config` JSON NULL COMMENT '费用查询配置',
  MODIFY COLUMN `advanced_config` JSON NULL COMMENT '高级配置（备用地址、提示词缓存等）',
  MODIFY COLUMN `proxy_url_cipher` TEXT NULL COMMENT '出站代理地址密文',
  MODIFY COLUMN `last_test_status` VARCHAR(32) NULL COMMENT '最近测试结果状态',
  MODIFY COLUMN `last_test_latency_ms` INT UNSIGNED NULL COMMENT '最近测试耗时（毫秒）',
  MODIFY COLUMN `last_test_error` VARCHAR(1024) NULL COMMENT '最近测试错误信息',
  MODIFY COLUMN `last_test_at` DATETIME(3) NULL COMMENT '最近测试时间',
  MODIFY COLUMN `last_cost_used` DECIMAL(20,8) NULL COMMENT '最近查询的已用额度',
  MODIFY COLUMN `last_cost_remaining` DECIMAL(20,8) NULL COMMENT '最近查询的剩余额度',
  MODIFY COLUMN `last_cost_currency` VARCHAR(12) NULL COMMENT '最近查询的币种',
  MODIFY COLUMN `last_cost_at` DATETIME(3) NULL COMMENT '最近费用查询时间',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  MODIFY COLUMN `deleted_at` DATETIME(3) NULL COMMENT '软删除时间',
  COMMENT='渠道表：上游 LLM 服务接入配置与路由属性';

ALTER TABLE `channel_types`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '渠道类型ID',
  MODIFY COLUMN `name` VARCHAR(96) NOT NULL COMMENT '类型名称',
  MODIFY COLUMN `code` VARCHAR(64) NOT NULL COMMENT '类型代码（唯一）',
  MODIFY COLUMN `config_json` JSON NOT NULL COMMENT '类型配置（端点、鉴权、费用/价格/音频适配器）',
  MODIFY COLUMN `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态（1=启用，0=禁用）',
  MODIFY COLUMN `built_in` TINYINT NOT NULL DEFAULT 0 COMMENT '是否内置（1=由 manifest/builtins.json 管理）',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  MODIFY COLUMN `deleted_at` DATETIME(3) NULL COMMENT '软删除时间',
  COMMENT='渠道类型表：声明式上游协议配置（内置类型由代码内 manifest 提供）';

ALTER TABLE `channel_models`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '渠道模型映射ID',
  MODIFY COLUMN `channel_id` BIGINT UNSIGNED NOT NULL COMMENT '所属渠道ID',
  MODIFY COLUMN `public_name` VARCHAR(191) NOT NULL COMMENT '对外模型名（客户端请求使用的名称）',
  MODIFY COLUMN `upstream_name` VARCHAR(191) NOT NULL COMMENT '上游模型名（转发到渠道时使用）',
  MODIFY COLUMN `discovered` TINYINT NOT NULL DEFAULT 1 COMMENT '是否来自自动发现（1=发现，0=手工添加）',
  MODIFY COLUMN `enabled` TINYINT NOT NULL DEFAULT 0 COMMENT '是否启用（1=参与路由）',
  MODIFY COLUMN `input_price` DECIMAL(20,8) NULL COMMENT '输入 token 单价（遗留，公共价格见 model_prices）',
  MODIFY COLUMN `cached_input_price` DECIMAL(20,8) NULL COMMENT '缓存输入 token 单价（遗留）',
  MODIFY COLUMN `output_price` DECIMAL(20,8) NULL COMMENT '输出 token 单价（遗留）',
  MODIFY COLUMN `health_score` INT NOT NULL DEFAULT 100 COMMENT '模型健康分（0-100，按请求成功/延迟增减）',
  MODIFY COLUMN `auto_disabled_at` DATETIME NULL COMMENT '模型级自动禁用时间',
  MODIFY COLUMN `auto_disabled_reason` VARCHAR(1024) NULL COMMENT '模型级自动禁用原因',
  MODIFY COLUMN `auto_disabled_source` VARCHAR(32) NULL COMMENT '模型级自动禁用来源',
  MODIFY COLUMN `last_test_endpoint` VARCHAR(32) NULL COMMENT '最近测试的端点',
  MODIFY COLUMN `last_test_status` VARCHAR(32) NULL COMMENT '最近测试结果状态',
  MODIFY COLUMN `last_test_latency_ms` INT UNSIGNED NULL COMMENT '最近测试耗时（毫秒）',
  MODIFY COLUMN `last_test_error` VARCHAR(1024) NULL COMMENT '最近测试错误信息',
  MODIFY COLUMN `last_test_at` DATETIME(3) NULL COMMENT '最近测试时间',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  MODIFY COLUMN `deleted_at` DATETIME(3) NULL COMMENT '软删除时间',
  COMMENT='渠道模型表：渠道与对外模型名的映射关系及模型级健康状态';

ALTER TABLE `channel_groups`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '渠道组ID',
  MODIFY COLUMN `name` VARCHAR(96) NOT NULL COMMENT '渠道组名称',
  MODIFY COLUMN `code` VARCHAR(64) NOT NULL COMMENT '渠道组代码（唯一）',
  MODIFY COLUMN `description` VARCHAR(255) NULL COMMENT '渠道组描述',
  MODIFY COLUMN `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态（1=启用，0=禁用）',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  MODIFY COLUMN `deleted_at` DATETIME(3) NULL COMMENT '软删除时间',
  COMMENT='渠道组表：渠道分组，用于密钥与用户的访问控制';

ALTER TABLE `channel_group_members`
  MODIFY COLUMN `channel_group_id` BIGINT UNSIGNED NOT NULL COMMENT '渠道组ID',
  MODIFY COLUMN `channel_id` BIGINT UNSIGNED NOT NULL COMMENT '渠道ID',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  COMMENT='渠道组成员表：渠道与渠道组的多对多关系';

ALTER TABLE `channel_credentials`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '渠道凭证ID',
  MODIFY COLUMN `channel_id` BIGINT UNSIGNED NOT NULL COMMENT '所属渠道ID',
  MODIFY COLUMN `key_prefix` VARCHAR(32) NOT NULL DEFAULT '' COMMENT '密钥前缀（展示用）',
  MODIFY COLUMN `key_hash` CHAR(64) NOT NULL DEFAULT '' COMMENT '密钥哈希（去重用）',
  MODIFY COLUMN `api_key_cipher` TEXT NOT NULL COMMENT '上游密钥密文',
  MODIFY COLUMN `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态（1=启用，0=禁用）',
  MODIFY COLUMN `auto_disabled_at` DATETIME(3) NULL COMMENT '自动禁用时间',
  MODIFY COLUMN `auto_disabled_reason` VARCHAR(1024) NULL COMMENT '自动禁用原因',
  MODIFY COLUMN `auto_disabled_status_code` SMALLINT UNSIGNED NULL COMMENT '触发自动禁用的上游状态码',
  MODIFY COLUMN `auto_disabled_source` VARCHAR(32) NULL COMMENT '自动禁用来源（relay/cost_query 等）',
  MODIFY COLUMN `last_cost_used` DECIMAL(20,8) NULL COMMENT '最近查询的已用额度',
  MODIFY COLUMN `last_cost_remaining` DECIMAL(20,8) NULL COMMENT '最近查询的剩余额度',
  MODIFY COLUMN `last_cost_currency` VARCHAR(12) NULL COMMENT '最近查询的币种',
  MODIFY COLUMN `last_cost_at` DATETIME(3) NULL COMMENT '最近费用查询时间',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  MODIFY COLUMN `deleted_at` DATETIME(3) NULL COMMENT '软删除时间',
  COMMENT='渠道凭证表：渠道的多密钥池，支持按密钥禁用与绑定';

ALTER TABLE `api_key_channel_credentials`
  MODIFY COLUMN `api_key_id` BIGINT UNSIGNED NOT NULL COMMENT 'API 密钥ID',
  MODIFY COLUMN `channel_id` BIGINT UNSIGNED NOT NULL COMMENT '渠道ID',
  MODIFY COLUMN `channel_credential_id` BIGINT UNSIGNED NOT NULL COMMENT '绑定的渠道凭证ID',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  COMMENT='密钥凭证绑定表：API 密钥在渠道上优先使用的上游凭证';

ALTER TABLE `api_key_models`
  MODIFY COLUMN `api_key_id` BIGINT UNSIGNED NOT NULL COMMENT 'API 密钥ID',
  MODIFY COLUMN `model_name` VARCHAR(191) NOT NULL COMMENT '允许调用的模型名',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  COMMENT='密钥模型授权表：密钥可调用的模型白名单（空=不限制）';

ALTER TABLE `api_key_channel_groups`
  MODIFY COLUMN `api_key_id` BIGINT UNSIGNED NOT NULL COMMENT 'API 密钥ID',
  MODIFY COLUMN `channel_group_id` BIGINT UNSIGNED NOT NULL COMMENT '渠道组ID',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  COMMENT='密钥渠道组授权表：密钥可访问的渠道组（空=不限制）';

-- ==================== 价格与费用 ====================
ALTER TABLE `model_prices`
  MODIFY COLUMN `public_name` VARCHAR(191) NOT NULL COMMENT '对外模型名（主键）',
  MODIFY COLUMN `billing_mode` VARCHAR(16) NOT NULL DEFAULT 'token' COMMENT '计费模式（token/request/rules）',
  MODIFY COLUMN `input_price` DECIMAL(20,8) NULL COMMENT '输入 token 单价',
  MODIFY COLUMN `cached_input_price` DECIMAL(20,8) NULL COMMENT '缓存命中输入 token 单价',
  MODIFY COLUMN `cache_write_price` DECIMAL(20,8) NULL COMMENT '缓存写入 token 单价',
  MODIFY COLUMN `output_price` DECIMAL(20,8) NULL COMMENT '输出 token 单价',
  MODIFY COLUMN `image_input_price` DECIMAL(20,8) NULL COMMENT '图片输入单价',
  MODIFY COLUMN `audio_input_price` DECIMAL(20,8) NULL COMMENT '音频输入 token 单价',
  MODIFY COLUMN `audio_output_price` DECIMAL(20,8) NULL COMMENT '音频输出 token 单价',
  MODIFY COLUMN `request_price` DECIMAL(20,8) NULL COMMENT '按次计费单价',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  MODIFY COLUMN `deleted_at` DATETIME(3) NULL COMMENT '软删除时间',
  COMMENT='公共模型价格表：按对外模型名维护的统一价格（渠道只作同步来源）';

ALTER TABLE `model_price_rules`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '价格规则ID',
  MODIFY COLUMN `channel_model_id` BIGINT UNSIGNED NOT NULL COMMENT '来源渠道模型ID',
  MODIFY COLUMN `model_name` VARCHAR(191) NOT NULL COMMENT '适用的对外模型名',
  MODIFY COLUMN `name` VARCHAR(96) NOT NULL COMMENT '规则名称',
  MODIFY COLUMN `source` VARCHAR(16) NOT NULL DEFAULT 'manual' COMMENT '来源（manual=手工，sync=同步）',
  MODIFY COLUMN `source_ref` VARCHAR(512) NULL COMMENT '来源引用（同步来源的标识）',
  MODIFY COLUMN `priority` INT NOT NULL DEFAULT 0 COMMENT '优先级（数值越大越优先匹配）',
  MODIFY COLUMN `currency` VARCHAR(12) NOT NULL DEFAULT 'USD' COMMENT '币种',
  MODIFY COLUMN `conditions_json` JSON NOT NULL COMMENT '匹配条件 JSON',
  MODIFY COLUMN `rates_json` JSON NOT NULL COMMENT '费率 JSON',
  MODIFY COLUMN `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态（1=启用，0=禁用）',
  MODIFY COLUMN `synced_at` DATETIME(3) NULL COMMENT '最近同步时间',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  MODIFY COLUMN `deleted_at` DATETIME(3) NULL COMMENT '软删除时间',
  COMMENT='模型价格规则表：按条件匹配的计费规则（rules 计费模式）';

ALTER TABLE `price_sources`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '价格来源ID',
  MODIFY COLUMN `name` VARCHAR(96) NOT NULL COMMENT '来源名称',
  MODIFY COLUMN `code` VARCHAR(64) NOT NULL COMMENT '来源代码（唯一）',
  MODIFY COLUMN `config_json` JSON NOT NULL COMMENT '来源配置（地址与价格适配器）',
  MODIFY COLUMN `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态（1=启用，0=禁用）',
  MODIFY COLUMN `built_in` TINYINT NOT NULL DEFAULT 0 COMMENT '是否内置（1=代码预置，不可删除）',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  MODIFY COLUMN `deleted_at` DATETIME(3) NULL COMMENT '软删除时间',
  COMMENT='价格来源表：外部公开价格同步源';

ALTER TABLE `channel_cost_snapshots`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '快照ID',
  MODIFY COLUMN `channel_id` BIGINT UNSIGNED NOT NULL COMMENT '渠道ID',
  MODIFY COLUMN `mode` VARCHAR(32) NOT NULL COMMENT '费用查询模式',
  MODIFY COLUMN `used_amount` DECIMAL(20,8) NULL COMMENT '已用额度',
  MODIFY COLUMN `remaining_amount` DECIMAL(20,8) NULL COMMENT '剩余额度',
  MODIFY COLUMN `currency` VARCHAR(12) NOT NULL DEFAULT 'USD' COMMENT '币种',
  MODIFY COLUMN `period_start` DATETIME(3) NULL COMMENT '账期开始时间',
  MODIFY COLUMN `period_end` DATETIME(3) NULL COMMENT '账期结束时间',
  MODIFY COLUMN `queried_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '查询时间',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  COMMENT='渠道费用快照表：渠道级费用查询历史';

ALTER TABLE `channel_credential_cost_snapshots`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '快照ID',
  MODIFY COLUMN `channel_credential_id` BIGINT UNSIGNED NOT NULL COMMENT '渠道凭证ID',
  MODIFY COLUMN `mode` VARCHAR(32) NOT NULL COMMENT '费用查询模式',
  MODIFY COLUMN `used_amount` DECIMAL(20,8) NULL COMMENT '已用额度',
  MODIFY COLUMN `remaining_amount` DECIMAL(20,8) NULL COMMENT '剩余额度',
  MODIFY COLUMN `currency` VARCHAR(12) NOT NULL DEFAULT 'USD' COMMENT '币种',
  MODIFY COLUMN `period_start` DATETIME(3) NULL COMMENT '账期开始时间',
  MODIFY COLUMN `period_end` DATETIME(3) NULL COMMENT '账期结束时间',
  MODIFY COLUMN `queried_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '查询时间',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  COMMENT='凭证费用快照表：渠道凭证级费用查询历史';

-- ==================== 用量与质量 ====================
ALTER TABLE `usage_logs`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '日志ID',
  MODIFY COLUMN `request_id` VARCHAR(64) NOT NULL COMMENT '请求唯一ID',
  MODIFY COLUMN `user_id` BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
  MODIFY COLUMN `api_key_id` BIGINT UNSIGNED NULL COMMENT 'API 密钥ID（测试请求为 NULL）',
  MODIFY COLUMN `channel_id` BIGINT UNSIGNED NULL COMMENT '实际使用的渠道ID',
  MODIFY COLUMN `channel_credential_id` BIGINT UNSIGNED NULL COMMENT '实际使用的渠道凭证ID',
  MODIFY COLUMN `endpoint` VARCHAR(64) NOT NULL COMMENT '网关端点（如 /v1/chat/completions、/audio/*）',
  MODIFY COLUMN `upstream_endpoint` VARCHAR(64) NULL COMMENT '实际上游端点（协议转换后）',
  MODIFY COLUMN `protocol_conversion` VARCHAR(64) NULL COMMENT '协议转换标识（如 responses_to_chat）',
  MODIFY COLUMN `client_ip` VARCHAR(45) NULL COMMENT '客户端 IP',
  MODIFY COLUMN `ip_location` VARCHAR(255) NULL COMMENT 'IP 归属地',
  MODIFY COLUMN `requested_model` VARCHAR(191) NOT NULL COMMENT '请求的对外模型名',
  MODIFY COLUMN `upstream_model` VARCHAR(191) NULL COMMENT '实际上游模型名',
  MODIFY COLUMN `reasoning_effort` VARCHAR(32) NULL COMMENT '推理强度（minimal/low/medium/high）',
  MODIFY COLUMN `http_status` SMALLINT UNSIGNED NOT NULL COMMENT '响应状态码',
  MODIFY COLUMN `is_stream` TINYINT NOT NULL DEFAULT 0 COMMENT '是否流式请求（1=流式）',
  MODIFY COLUMN `input_tokens` BIGINT UNSIGNED NULL COMMENT '输入 token 数',
  MODIFY COLUMN `cached_input_tokens` BIGINT UNSIGNED NULL COMMENT '缓存命中输入 token 数',
  MODIFY COLUMN `output_tokens` BIGINT UNSIGNED NULL COMMENT '输出 token 数',
  MODIFY COLUMN `total_tokens` BIGINT UNSIGNED NULL COMMENT '总 token 数',
  MODIFY COLUMN `estimated_cost` DECIMAL(20,8) NULL COMMENT '估算费用',
  MODIFY COLUMN `billing_details_json` TEXT NULL COMMENT '计费明细 JSON（规则、币种、各项费用）',
  MODIFY COLUMN `duration_ms` BIGINT UNSIGNED NOT NULL COMMENT '总耗时（毫秒）',
  MODIFY COLUMN `first_token_ms` BIGINT UNSIGNED NULL COMMENT '首 token 耗时（毫秒）',
  MODIFY COLUMN `attempts` TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '渠道重试次数',
  MODIFY COLUMN `error_message` VARCHAR(1024) NULL COMMENT '错误信息',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  COMMENT='用量日志表：每次网关调用一条记录，计费与统计的事实来源';

ALTER TABLE `model_quality_events`
  MODIFY COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '事件ID',
  MODIFY COLUMN `request_id` VARCHAR(64) NOT NULL COMMENT '关联的请求ID',
  MODIFY COLUMN `channel_id` BIGINT UNSIGNED NOT NULL COMMENT '渠道ID',
  MODIFY COLUMN `credential_id` BIGINT UNSIGNED NOT NULL COMMENT '渠道凭证ID',
  MODIFY COLUMN `requested_model` VARCHAR(191) NOT NULL COMMENT '请求的对外模型名',
  MODIFY COLUMN `expected_model` VARCHAR(191) NOT NULL COMMENT '期望的上游模型名',
  MODIFY COLUMN `observed_model` VARCHAR(191) NOT NULL DEFAULT '' COMMENT '响应中观察到的模型名',
  MODIFY COLUMN `reasons_json` JSON NOT NULL COMMENT '异常原因列表 JSON',
  MODIFY COLUMN `question_chars` INT UNSIGNED NOT NULL COMMENT '提问字符数',
  MODIFY COLUMN `answer_chars` INT UNSIGNED NOT NULL COMMENT '回答字符数',
  MODIFY COLUMN `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  COMMENT='模型质量事件表：异步检测到的模型质量异常（疑似降智/换模）';

-- ==================== 系统配置 ====================
ALTER TABLE `system_settings`
  MODIFY COLUMN `setting_key` VARCHAR(64) NOT NULL COMMENT '设置键',
  MODIFY COLUMN `value_json` JSON NOT NULL COMMENT '设置值 JSON',
  MODIFY COLUMN `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  MODIFY COLUMN `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  COMMENT='系统设置表：键值型系统级配置（容灾、敏感词、站点信息等）';

-- +goose Down
-- 注释不影响数据结构语义，回滚仅作形式还原（不还原注释文本）。
SELECT 1;
