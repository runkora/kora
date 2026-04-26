-- +goose Up
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_inference_usage_analytics_personal
    ON inference_usage (tenant_id, principal_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_inference_usage_analytics_personal;
-- +goose StatementEnd
