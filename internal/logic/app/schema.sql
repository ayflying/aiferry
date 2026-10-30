-- AiFerry 子节点内置 SQLite 结构。
-- 说明：
--  1. 配置类表由主节点快照下发并 upsert 覆盖（列白名单见 internal/logic/cluster/apply.go）；
--     运行态列（健康分、冷却、消耗累计）只在本节点维护，快照不会覆盖。
--  2. 时间列统一用 TEXT，由 GoFrame sqlite 驱动按 time.Time 双向转换。
--  3. 本文件只用 IF NOT EXISTS，可重复执行；新增列时以新增语句追加，不要改写历史语句。

CREATE TABLE IF NOT EXISTS channels (
	id                        INTEGER PRIMARY KEY,
	created_by_user_id        INTEGER NOT NULL DEFAULT 0,
	name                      TEXT    NOT NULL DEFAULT '',
	type                      TEXT    NOT NULL DEFAULT '',
	base_url                  TEXT    NOT NULL DEFAULT '',
	api_key_cipher            TEXT,
	management_key_cipher     TEXT,
	organization_id           TEXT,
	project_id                TEXT,
	status                    INTEGER NOT NULL DEFAULT 1,
	auto_disabled_at          TEXT,
	auto_disabled_reason      TEXT,
	auto_disabled_status_code INTEGER NOT NULL DEFAULT 0,
	auto_disabled_source      TEXT,
	priority                  INTEGER NOT NULL DEFAULT 0,
	weight                    INTEGER NOT NULL DEFAULT 0,
	health_check_model_id     INTEGER NOT NULL DEFAULT 0,
	auto_disable_enabled      INTEGER NOT NULL DEFAULT 0,
	cost_query_mode           TEXT,
	cost_query_config         TEXT,
	advanced_config           TEXT,
	proxy_url_cipher          TEXT,
	last_test_status          TEXT,
	last_test_latency_ms      INTEGER NOT NULL DEFAULT 0,
	last_test_error           TEXT,
	last_test_at              TEXT,
	last_cost_used            REAL    NOT NULL DEFAULT 0,
	last_cost_remaining       REAL    NOT NULL DEFAULT 0,
	last_cost_currency        TEXT,
	last_cost_at              TEXT,
	created_at                TEXT,
	updated_at                TEXT,
	deleted_at                TEXT
);

CREATE TABLE IF NOT EXISTS channel_credentials (
	id                        INTEGER PRIMARY KEY,
	channel_id                INTEGER NOT NULL DEFAULT 0,
	key_prefix                TEXT,
	key_hash                  TEXT,
	api_key_cipher            TEXT,
	management_key_cipher     TEXT,
	status                    INTEGER NOT NULL DEFAULT 1,
	auto_disabled_at          TEXT,
	auto_disabled_reason      TEXT,
	auto_disabled_status_code INTEGER NOT NULL DEFAULT 0,
	auto_disabled_source      TEXT,
	last_cost_used            REAL    NOT NULL DEFAULT 0,
	last_cost_remaining       REAL    NOT NULL DEFAULT 0,
	last_cost_currency        TEXT,
	last_cost_at              TEXT,
	created_at                TEXT,
	updated_at                TEXT,
	deleted_at                TEXT
);

CREATE TABLE IF NOT EXISTS channel_models (
	id                     INTEGER PRIMARY KEY,
	channel_id             INTEGER NOT NULL DEFAULT 0,
	public_name            TEXT    NOT NULL DEFAULT '',
	upstream_name          TEXT,
	discovered             INTEGER NOT NULL DEFAULT 0,
	enabled                INTEGER NOT NULL DEFAULT 1,
	input_price            REAL,
	cached_input_price     REAL,
	output_price           REAL,
	health_score           REAL    NOT NULL DEFAULT 100,
	auto_disabled_at       TEXT,
	auto_disabled_reason   TEXT,
	auto_disabled_source   TEXT,
	closed_windows_json    TEXT,
	last_test_endpoint     TEXT,
	last_test_status       TEXT,
	last_test_latency_ms   INTEGER NOT NULL DEFAULT 0,
	last_test_error        TEXT,
	last_test_at           TEXT,
	created_at             TEXT,
	updated_at             TEXT,
	deleted_at             TEXT
);

CREATE TABLE IF NOT EXISTS channel_model_credentials (
	id                  INTEGER PRIMARY KEY,
	channel_id          INTEGER NOT NULL DEFAULT 0,
	channel_model_id    INTEGER NOT NULL DEFAULT 0,
	channel_credential_id INTEGER NOT NULL DEFAULT 0,
	health_score        REAL    NOT NULL DEFAULT 100,
	cooldown_until      TEXT,
	last_error          TEXT,
	created_at          TEXT,
	updated_at          TEXT
);

CREATE TABLE IF NOT EXISTS channel_groups (
	id          INTEGER PRIMARY KEY,
	name        TEXT    NOT NULL DEFAULT '',
	code        TEXT,
	description TEXT,
	status      INTEGER NOT NULL DEFAULT 1,
	created_at  TEXT,
	updated_at  TEXT,
	deleted_at  TEXT
);

CREATE TABLE IF NOT EXISTS channel_group_members (
	channel_group_id INTEGER NOT NULL,
	channel_id       INTEGER NOT NULL,
	created_at       TEXT,
	PRIMARY KEY (channel_group_id, channel_id)
);

CREATE TABLE IF NOT EXISTS channel_types (
	id          INTEGER PRIMARY KEY,
	name        TEXT    NOT NULL DEFAULT '',
	code        TEXT    NOT NULL DEFAULT '',
	config_json TEXT,
	status      INTEGER NOT NULL DEFAULT 1,
	built_in    INTEGER NOT NULL DEFAULT 0,
	created_at  TEXT,
	updated_at  TEXT,
	deleted_at  TEXT
);

CREATE TABLE IF NOT EXISTS model_prices (
	public_name        TEXT PRIMARY KEY,
	billing_mode       TEXT,
	input_price        REAL,
	cached_input_price REAL,
	cache_write_price  REAL,
	output_price       REAL,
	image_input_price  REAL,
	audio_input_price  REAL,
	audio_output_price REAL,
	request_price      REAL,
	created_at         TEXT,
	updated_at         TEXT,
	deleted_at         TEXT
);

CREATE TABLE IF NOT EXISTS model_price_rules (
	id               INTEGER PRIMARY KEY,
	channel_model_id INTEGER NOT NULL DEFAULT 0,
	model_name       TEXT    NOT NULL DEFAULT '',
	name             TEXT,
	source           TEXT,
	source_ref       TEXT,
	priority         INTEGER NOT NULL DEFAULT 0,
	currency         TEXT,
	conditions_json  TEXT,
	rates_json       TEXT,
	status           INTEGER NOT NULL DEFAULT 1,
	synced_at        TEXT,
	created_at       TEXT,
	updated_at       TEXT,
	deleted_at       TEXT
);

CREATE TABLE IF NOT EXISTS price_sources (
	id          INTEGER PRIMARY KEY,
	name        TEXT    NOT NULL DEFAULT '',
	code        TEXT    NOT NULL DEFAULT '',
	config_json TEXT,
	status      INTEGER NOT NULL DEFAULT 1,
	built_in    INTEGER NOT NULL DEFAULT 0,
	created_at  TEXT,
	updated_at  TEXT,
	deleted_at  TEXT
);

CREATE TABLE IF NOT EXISTS system_settings (
	setting_key TEXT PRIMARY KEY,
	value_json  TEXT,
	created_at  TEXT,
	updated_at  TEXT
);

CREATE TABLE IF NOT EXISTS users (
	id                 INTEGER PRIMARY KEY,
	name               TEXT,
	email              TEXT,
	role               TEXT,
	status             INTEGER NOT NULL DEFAULT 1,
	balance            REAL    NOT NULL DEFAULT 0,
	identity_provider  TEXT,
	identity_subject   TEXT,
	avatar_url         TEXT,
	groups_json        TEXT,
	last_login_at      TEXT,
	created_at         TEXT,
	updated_at         TEXT,
	deleted_at         TEXT
);

CREATE TABLE IF NOT EXISTS api_keys (
	id                 INTEGER PRIMARY KEY,
	user_id            INTEGER NOT NULL DEFAULT 0,
	name               TEXT,
	key_prefix         TEXT,
	key_hash           TEXT,
	key_cipher         TEXT,
	status             INTEGER NOT NULL DEFAULT 1,
	spend_limit        REAL,
	daily_spend_limit  REAL,
	spent_amount       REAL    NOT NULL DEFAULT 0,
	daily_spent_amount REAL    NOT NULL DEFAULT 0,
	daily_spend_date   TEXT,
	expires_at         TEXT,
	last_used_at       TEXT,
	created_at         TEXT,
	updated_at         TEXT,
	deleted_at         TEXT
);

CREATE TABLE IF NOT EXISTS api_key_models (
	api_key_id INTEGER NOT NULL,
	model_name TEXT    NOT NULL,
	created_at TEXT,
	PRIMARY KEY (api_key_id, model_name)
);

CREATE TABLE IF NOT EXISTS api_key_channel_groups (
	api_key_id       INTEGER NOT NULL,
	channel_group_id INTEGER NOT NULL,
	created_at       TEXT,
	PRIMARY KEY (api_key_id, channel_group_id)
);

CREATE TABLE IF NOT EXISTS api_key_channel_credentials (
	api_key_id          INTEGER NOT NULL,
	channel_id          INTEGER NOT NULL,
	channel_credential_id INTEGER NOT NULL DEFAULT 0,
	created_at          TEXT,
	updated_at          TEXT,
	PRIMARY KEY (api_key_id, channel_id)
);

-- 子节点本地待回传用量。主节点确认（ack）后即删除，本地不保留历史；
-- id 自增序即回传游标，node_id 供主节点记账时标识来源节点。
CREATE TABLE IF NOT EXISTS usage_logs (
	id                    INTEGER PRIMARY KEY AUTOINCREMENT,
	request_id            TEXT    NOT NULL,
	user_id               INTEGER NOT NULL DEFAULT 0,
	api_key_id            INTEGER NOT NULL DEFAULT 0,
	channel_id            INTEGER,
	channel_credential_id INTEGER,
	endpoint              TEXT    NOT NULL DEFAULT '',
	requested_model       TEXT    NOT NULL DEFAULT '',
	upstream_model        TEXT,
	http_status           INTEGER NOT NULL DEFAULT 0,
	is_stream             INTEGER NOT NULL DEFAULT 0,
	input_tokens          INTEGER,
	cached_input_tokens   INTEGER,
	output_tokens         INTEGER,
	total_tokens          INTEGER,
	estimated_cost        REAL,
	billing_details_json  TEXT,
	duration_ms           INTEGER NOT NULL DEFAULT 0,
	first_token_ms        INTEGER,
	attempts              INTEGER NOT NULL DEFAULT 1,
	attempt_flow_json     TEXT,
	error_message         TEXT,
	node_id               INTEGER NOT NULL DEFAULT 0,
	created_at            TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_usage_logs_request ON usage_logs (request_id);

-- 本地运行态：渠道/凭据的自动禁用与冷却只在本节点生效，
-- 事件通过 /internal/cluster/events 上报，主节点决策后再由快照回写。
CREATE TABLE IF NOT EXISTS local_channel_state (
	channel_id     INTEGER PRIMARY KEY,
	disabled       INTEGER NOT NULL DEFAULT 0,
	cooldown_until TEXT,
	failures       INTEGER NOT NULL DEFAULT 0,
	updated_at     TEXT
);

CREATE TABLE IF NOT EXISTS local_credential_state (
	channel_credential_id INTEGER PRIMARY KEY,
	disabled              INTEGER NOT NULL DEFAULT 0,
	cooldown_until        TEXT,
	failures              INTEGER NOT NULL DEFAULT 0,
	updated_at            TEXT
);

CREATE TABLE IF NOT EXISTS local_combo_state (
	channel_model_id      INTEGER NOT NULL,
	channel_credential_id INTEGER NOT NULL,
	health_score          REAL    NOT NULL DEFAULT 100,
	cooldown_until        TEXT,
	last_error            TEXT,
	updated_at            TEXT,
	PRIMARY KEY (channel_model_id, channel_credential_id)
);

-- 待上报的节点事件（自动禁用、组合冷却、主密钥自检失败等）。
CREATE TABLE IF NOT EXISTS local_events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	kind       TEXT    NOT NULL DEFAULT '',
	ref_id     INTEGER NOT NULL DEFAULT 0,
	payload    TEXT,
	created_at TEXT
);

-- 节点级键值状态：配置版本、最后同步时间、最后错误、节点 ID 等。
CREATE TABLE IF NOT EXISTS sync_state (
	state_key TEXT PRIMARY KEY,
	value     TEXT
);