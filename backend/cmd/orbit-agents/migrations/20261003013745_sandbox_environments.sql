-- +goose Up
-- +goose StatementBegin
-- Environments become prepackaged sandboxes (docs/adr/2026-10-03-sandbox-environments.md).
-- The old egress bundles and their agent links are dropped, not carried over.
ALTER TABLE agent_types DROP COLUMN environment_id;
DROP TABLE environments;

-- sandbox_image names a SandboxImage in the tenant namespace; empty is the base image.
-- cpu, memory and disk are Kubernetes quantities.
CREATE TABLE environments (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    sandbox_image TEXT DEFAULT '' NOT NULL,
    cpu TEXT NOT NULL,
    memory TEXT NOT NULL,
    disk TEXT NOT NULL,
    egress_hosts TEXT[] DEFAULT '{}' NOT NULL,
    connectors JSONB DEFAULT '[]'::jsonb NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT environments_pkey PRIMARY KEY (id),
    CONSTRAINT environments_tenant_id_name_key UNIQUE (tenant_id, name)
);

-- An agent's environments; is_default marks the one a sandbox starts in. An
-- attached environment cannot be deleted. An agent with none uses kora-base.
CREATE TABLE agent_environments (
    agent_type_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    is_default BOOLEAN DEFAULT false NOT NULL,
    CONSTRAINT agent_environments_pkey PRIMARY KEY (agent_type_id, environment_id),
    CONSTRAINT agent_environments_agent_type_id_fkey FOREIGN KEY (agent_type_id) REFERENCES agent_types(id) ON DELETE CASCADE,
    CONSTRAINT agent_environments_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments(id)
);

CREATE UNIQUE INDEX idx_agent_environments_default ON agent_environments (agent_type_id) WHERE is_default;

CREATE INDEX idx_agent_environments_environment_id ON agent_environments (environment_id);

-- The environment of the run's current sandbox, kept so a replacement after a sandbox loss uses it again.
ALTER TABLE runs ADD COLUMN environment_id TEXT REFERENCES environments(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN environment_id;
DROP TABLE agent_environments;
DROP TABLE environments;

CREATE TABLE environments (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    egress_hosts TEXT[] DEFAULT '{}' NOT NULL,
    connectors JSONB DEFAULT '[]'::jsonb NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT environments_pkey PRIMARY KEY (id),
    CONSTRAINT environments_tenant_id_name_key UNIQUE (tenant_id, name)
);

ALTER TABLE agent_types ADD COLUMN environment_id TEXT REFERENCES environments(id) ON DELETE SET NULL;
CREATE INDEX idx_agent_types_environment_id ON agent_types (environment_id) WHERE environment_id IS NOT NULL;
-- +goose StatementEnd
