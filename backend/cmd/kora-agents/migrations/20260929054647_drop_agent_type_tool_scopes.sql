-- +goose Up
-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN tool_scopes;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types ADD COLUMN tool_scopes TEXT[] DEFAULT '{}' NOT NULL;
-- +goose StatementEnd
