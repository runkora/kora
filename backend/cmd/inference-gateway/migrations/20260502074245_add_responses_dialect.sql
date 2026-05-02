-- +goose Up
-- +goose StatementBegin

-- Add 'responses' to the api_dialect CHECK constraint so that POST /v1/responses
-- usage records can be stored with their correct dialect value.
ALTER TABLE inference_usage
    DROP CONSTRAINT IF EXISTS inference_usage_api_dialect_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_api_dialect_check
    CHECK (api_dialect IN ('openai', 'anthropic', 'responses'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE inference_usage
    DROP CONSTRAINT IF EXISTS inference_usage_api_dialect_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_api_dialect_check
    CHECK (api_dialect IN ('openai', 'anthropic'));

-- +goose StatementEnd
