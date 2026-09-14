-- +goose Up
-- +goose StatementBegin
CREATE TABLE policy_role_scopes (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    role TEXT NOT NULL,
    scopes JSONB DEFAULT '[]'::jsonb NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    budget JSONB,
    CONSTRAINT policy_role_scopes_pkey PRIMARY KEY (id),
    CONSTRAINT policy_role_scopes_tenant_id_role_key UNIQUE (tenant_id, role)
);

CREATE INDEX idx_policy_role_scopes_tenant_id ON policy_role_scopes (tenant_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS policy_role_scopes;
-- +goose StatementEnd
