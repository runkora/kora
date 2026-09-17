-- +goose Up
-- +goose StatementBegin
CREATE TABLE mcp_tools (
    tenant_id TEXT NOT NULL,
    server_slug TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    scope TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    input_schema JSONB DEFAULT '{}'::jsonb NOT NULL,
    discovered_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT mcp_tools_pkey PRIMARY KEY (tenant_id, server_slug, tool_name)
);

CREATE INDEX idx_mcp_tools_tenant_server ON mcp_tools (tenant_id, server_slug);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS mcp_tools;
-- +goose StatementEnd
