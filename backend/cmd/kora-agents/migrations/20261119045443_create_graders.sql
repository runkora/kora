-- +goose Up
-- +goose StatementBegin
-- A Grader version is content-addressed: version is the sha256 of spec, the Grader's model,
-- threshold, appliesWhen and questions (docs/adr/2026-11-19-graders.md).
CREATE TABLE grader_versions (
    version TEXT NOT NULL,
    spec JSONB NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT grader_versions_pkey PRIMARY KEY (version)
);

-- A Grader projected from a resource. min_pass_rate is a percent, NULL when unset.
-- Graders have no API writes, so every row carries its resource's UID and generation.
CREATE TABLE graders (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    agent_type_id TEXT NOT NULL,
    description TEXT NOT NULL,
    min_pass_rate SMALLINT CHECK (min_pass_rate BETWEEN 0 AND 100),
    current_version TEXT NOT NULL,
    resource_uid TEXT NOT NULL,
    resource_generation BIGINT NOT NULL,
    repository TEXT,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT graders_pkey PRIMARY KEY (id),
    CONSTRAINT graders_tenant_name_key UNIQUE (tenant_id, name),
    -- Only a failed first provisioning hard-deletes an agent. Its Grader is applied again once the agent is back.
    CONSTRAINT graders_agent_type_id_fkey FOREIGN KEY (agent_type_id) REFERENCES agent_types(id) ON DELETE CASCADE,
    CONSTRAINT graders_current_version_fkey FOREIGN KEY (current_version) REFERENCES grader_versions(version)
);

CREATE INDEX idx_graders_agent_type_id ON graders (agent_type_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE graders;
DROP TABLE grader_versions;
-- +goose StatementEnd
