-- +goose Up
-- The state of a source's Gateway session: connected, or rejected with the reason Discord gave.
-- +goose StatementBegin
ALTER TABLE webhook_sources
    ADD COLUMN gateway_status TEXT,
    ADD COLUMN gateway_reason TEXT,
    ADD COLUMN gateway_status_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE webhook_sources
    DROP COLUMN IF EXISTS gateway_status,
    DROP COLUMN IF EXISTS gateway_reason,
    DROP COLUMN IF EXISTS gateway_status_at;
-- +goose StatementEnd
