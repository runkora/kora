-- +goose Up
-- +goose StatementBegin
-- The person or agent who sent a message. Null for a trigger's message. See docs/adr/2026-11-11-run-page-record.md.
ALTER TABLE run_messages
    ADD COLUMN sender_id TEXT,
    ADD COLUMN sender_type TEXT,
    ADD COLUMN sender_name TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE run_messages
    DROP COLUMN sender_id,
    DROP COLUMN sender_type,
    DROP COLUMN sender_name;
-- +goose StatementEnd
