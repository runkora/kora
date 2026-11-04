-- +goose Up
-- +goose StatementBegin
-- Contract step for #398 (docs/adr/2026-10-03-sandbox-environments.md). Agents
-- now attach environments through agent_environments; no code has read or
-- written this column for a minor release.
ALTER TABLE agent_types DROP COLUMN environment_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types ADD COLUMN environment_id TEXT REFERENCES environments(id) ON DELETE SET NULL;

CREATE INDEX idx_agent_types_environment_id ON agent_types (environment_id) WHERE environment_id IS NOT NULL;
-- +goose StatementEnd
