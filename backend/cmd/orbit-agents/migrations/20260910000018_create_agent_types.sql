-- +goose Up
-- +goose StatementBegin
CREATE TABLE agent_types (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    tool_scopes TEXT[] DEFAULT '{}' NOT NULL,
    max_concurrent_runs_per_pod INTEGER DEFAULT 10 NOT NULL,
    state TEXT DEFAULT 'provisioning' NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    grant_scopes TEXT[] DEFAULT '{}' NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    system_prompt TEXT DEFAULT '' NOT NULL,
    framework TEXT DEFAULT '' NOT NULL,
    model_id TEXT,
    connectivity JSONB DEFAULT '{"mode": "none"}'::jsonb NOT NULL,
    connectors JSONB DEFAULT '[]'::jsonb NOT NULL,
    environment_id TEXT,
    mcp_tools TEXT[] DEFAULT '{}' NOT NULL,
    identity_mode TEXT DEFAULT 'autonomous' NOT NULL,
    CONSTRAINT agent_types_pkey PRIMARY KEY (id),
    CONSTRAINT agent_types_tenant_id_slug_key UNIQUE (tenant_id, slug),
    CONSTRAINT agent_types_identity_mode_check CHECK (identity_mode IN ('autonomous', 'delegated')),
    CONSTRAINT agent_types_state_check CHECK (state IN ('provisioning', 'active', 'deleting')),
    CONSTRAINT agent_types_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE SET NULL
);

CREATE INDEX idx_agent_types_environment_id ON agent_types (environment_id) WHERE environment_id IS NOT NULL;

CREATE INDEX idx_agent_types_tenant_id ON agent_types (tenant_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS agent_types;
-- +goose StatementEnd
