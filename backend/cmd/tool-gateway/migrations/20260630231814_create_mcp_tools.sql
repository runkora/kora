-- +goose Up
-- +goose StatementBegin
-- mcp_tools is the tool-gateway's read-only catalog: the human-facing
-- projection of what each MCP server exposes, rebuilt on each introspection.
-- It is DERIVED state — the policy scope vocabulary remains the source of truth
-- for authorisation; this table only carries display metadata (description,
-- input schema) for the admin UI. tenant_id is '' for global (orbit-system)
-- servers, whose tools appear in every tenant's catalog.
-- The natural key (tenant_id, server_slug, tool_name) IS the primary key — no
-- surrogate id, since the gateway is the single writer and a tool is uniquely
-- identified by which server (in which tenant) exposes it.
CREATE TABLE IF NOT EXISTS mcp_tools (
    tenant_id     TEXT        NOT NULL,
    server_slug   TEXT        NOT NULL,
    tool_name     TEXT        NOT NULL,
    scope         TEXT        NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    input_schema  JSONB       NOT NULL DEFAULT '{}',
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, server_slug, tool_name)
);
CREATE INDEX IF NOT EXISTS idx_mcp_tools_tenant_server ON mcp_tools (tenant_id, server_slug);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS mcp_tools;
-- +goose StatementEnd
