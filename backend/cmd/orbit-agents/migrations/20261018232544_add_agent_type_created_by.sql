-- +goose Up
-- +goose StatementBegin
-- The token subject that created the agent type. Authz grants the creator owner access to the type and its runs.
-- Empty matches no caller.
ALTER TABLE agent_types ADD COLUMN created_by TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN created_by;
-- +goose StatementEnd
