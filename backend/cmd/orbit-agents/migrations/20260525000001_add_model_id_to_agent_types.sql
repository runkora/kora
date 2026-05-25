-- +goose Up
-- +goose StatementBegin
ALTER TABLE agent_types
    ADD COLUMN model_id TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types
    DROP COLUMN model_id;
-- +goose StatementEnd
