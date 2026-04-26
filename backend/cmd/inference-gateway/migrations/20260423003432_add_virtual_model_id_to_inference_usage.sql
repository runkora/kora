-- +goose Up
-- +goose StatementBegin

-- Add virtual_model_id to inference_usage so analytics can filter and group
-- by the consumer-facing virtual model identity (e.g. "anthropic/claude-sonnet-4-6")
-- rather than the raw provider model ID (e.g. "anthropic.claude-3-5-sonnet-20241022-v2:0").
-- NULL for records written before this migration and for requests that fail
-- before a virtual model is resolved.
ALTER TABLE inference_usage
    ADD COLUMN IF NOT EXISTS virtual_model_id TEXT NULL;

CREATE INDEX IF NOT EXISTS idx_inference_usage_virtual_model
    ON inference_usage (tenant_id, virtual_model_id, created_at DESC)
    WHERE virtual_model_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_inference_usage_virtual_model;

ALTER TABLE inference_usage
    DROP COLUMN IF EXISTS virtual_model_id;

-- +goose StatementEnd
