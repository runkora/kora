-- +goose Up
-- Agent type drafts: a draft is a saved form, not an agent that happens to be
-- unfinished. It carries no constraints because a draft has no invariants —
-- payload is a partially-filled CreateAgentTypeRequest stored opaquely. A draft
-- can never hold access: grants read agent_types, and a draft is not there.
-- See docs/adr/2026-07-17-agent-builder.md §2.
-- +goose StatementBegin
CREATE TABLE agent_type_drafts (
    id         TEXT        PRIMARY KEY,
    tenant_id  TEXT        NOT NULL,
    created_by TEXT        NOT NULL,
    payload    JSONB       NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- Drafts are personal: every read and write filters to (tenant_id, created_by);
-- listing orders by most recently touched.
-- +goose StatementBegin
CREATE INDEX idx_agent_type_drafts_owner
    ON agent_type_drafts (tenant_id, created_by, updated_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_agent_type_drafts_owner;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS agent_type_drafts;
-- +goose StatementEnd
