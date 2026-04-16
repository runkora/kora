-- +goose Up
-- +goose StatementBegin

-- Add model_family column for filtering usage records by virtual model family.
-- NULL is allowed so the column is backward-compatible with rows written before
-- this migration (and with failure records where routing never selected a target).
ALTER TABLE inference_usage
    ADD COLUMN IF NOT EXISTS model_family TEXT NULL;

-- Support filtering a tenant's usage by model family sorted by recency.
CREATE INDEX IF NOT EXISTS idx_inference_usage_model_family
    ON inference_usage (tenant_id, model_family, created_at DESC)
    WHERE model_family IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_inference_usage_model_family;

ALTER TABLE inference_usage
    DROP COLUMN IF EXISTS model_family;

-- +goose StatementEnd
