-- +goose Up
-- +goose StatementBegin
ALTER TABLE agent_types ADD COLUMN grant_scopes TEXT[] NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN grant_scopes;
-- +goose StatementEnd
