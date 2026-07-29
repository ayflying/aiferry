-- +goose Up
CREATE INDEX idx_usage_logs_created_cost ON usage_logs (created_at, estimated_cost);
CREATE INDEX idx_usage_logs_user_summary ON usage_logs (user_id, created_at, http_status, input_tokens, output_tokens, total_tokens, estimated_cost);

-- +goose Down
DROP INDEX idx_usage_logs_user_summary ON usage_logs;
DROP INDEX idx_usage_logs_created_cost ON usage_logs;
