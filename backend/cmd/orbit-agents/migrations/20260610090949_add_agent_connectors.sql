-- +goose Up
-- +goose StatementBegin
-- connectors records which registered credential connections an agent type may
-- use and for which actions. It compiles to connection:<name>:<action> grants,
-- the credential parallel to connectivity's egress:<host>:connect. JSONB array of
-- {connection, actions[]}; default empty so existing rows have no connectors.
ALTER TABLE agent_types
    ADD COLUMN connectors JSONB NOT NULL DEFAULT '[]'::jsonb;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN connectors;
-- +goose StatementEnd
