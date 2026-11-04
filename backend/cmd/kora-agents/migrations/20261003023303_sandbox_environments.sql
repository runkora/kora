-- +goose Up
-- +goose StatementBegin
-- Environments become prepackaged sandboxes (docs/adr/2026-10-03-sandbox-environments.md).
-- Expand only. agent_types.environment_id stays for the previous release.
-- sandbox_image names a SandboxImage in the tenant namespace. kora-base is the base image.
-- cpu, memory and disk are Kubernetes quantities. The defaults match the chart's default size.
ALTER TABLE environments ADD COLUMN sandbox_image TEXT DEFAULT 'kora-base' NOT NULL;
ALTER TABLE environments ADD COLUMN cpu TEXT DEFAULT '2' NOT NULL;
ALTER TABLE environments ADD COLUMN memory TEXT DEFAULT '4Gi' NOT NULL;
ALTER TABLE environments ADD COLUMN disk TEXT DEFAULT '10Gi' NOT NULL;

-- An agent's environments. is_default marks the one a sandbox starts in.
-- An attached environment cannot be deleted. An agent with none uses kora-base.
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

-- The environment of the run's current sandbox. A replacement after a sandbox loss uses it again.
ALTER TABLE runs ADD COLUMN environment_id TEXT REFERENCES environments(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN environment_id;
DROP TABLE agent_environments;
ALTER TABLE environments DROP COLUMN disk;
ALTER TABLE environments DROP COLUMN memory;
ALTER TABLE environments DROP COLUMN cpu;
ALTER TABLE environments DROP COLUMN sandbox_image;
-- +goose StatementEnd
