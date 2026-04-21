-- +goose Up
-- +goose StatementBegin
ALTER TABLE inference_usage
    ADD COLUMN cache_read_input_tokens     INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN cache_creation_input_tokens INTEGER NOT NULL DEFAULT 0;

CREATE INDEX idx_inference_usage_cache_tokens
    ON inference_usage (tenant_id, cache_read_input_tokens, created_at DESC)
    WHERE cache_read_input_tokens > 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_inference_usage_cache_tokens;

ALTER TABLE inference_usage
    DROP COLUMN IF EXISTS cache_read_input_tokens,
    DROP COLUMN IF EXISTS cache_creation_input_tokens;
-- +goose StatementEnd
