-- +goose Up
-- +goose StatementBegin
-- Connectivity posture for an agent type: the outbound egress reachability gate
-- ({"mode":"none|whitelist|open","hosts":[...]}), compiled to egress:<host>:connect
-- scopes on the agent's policy grant. Defaults to deny-by-default (none).
ALTER TABLE agent_types
    ADD COLUMN connectivity JSONB NOT NULL DEFAULT '{"mode":"none"}'::jsonb;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN connectivity;
-- +goose StatementEnd
