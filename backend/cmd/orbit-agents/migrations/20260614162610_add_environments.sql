-- +goose Up
-- Environments: tenant-scoped, UI-managed bundles of egress hosts + connectors
-- that agent types reference. See
-- docs/adr/2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md.
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS environments (
    id           TEXT        PRIMARY KEY,
    tenant_id    TEXT        NOT NULL,
    name         TEXT        NOT NULL,
    description  TEXT        NOT NULL DEFAULT '',
    egress_hosts TEXT[]      NOT NULL DEFAULT '{}',
    connectors   JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);
-- +goose StatementEnd

-- Agent types reference at most one environment. ON DELETE SET NULL detaches
-- referencing agents when an environment is removed (their grants then
-- re-reconcile to their own config).
-- +goose StatementBegin
ALTER TABLE agent_types
    ADD COLUMN environment_id TEXT REFERENCES environments(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- Supports the reverse lookup the environment service uses to re-reconcile every
-- referencing agent when an environment changes.
-- +goose StatementBegin
CREATE INDEX idx_agent_types_environment_id
    ON agent_types (environment_id) WHERE environment_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_agent_types_environment_id;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN IF EXISTS environment_id;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS environments;
-- +goose StatementEnd
