-- +goose Up
ALTER TABLE inference_usage
    ADD COLUMN IF NOT EXISTS failure_details JSONB;

-- +goose Down
ALTER TABLE inference_usage
    DROP COLUMN IF EXISTS failure_details;
