-- +goose Up
-- +goose StatementBegin
CREATE TABLE policy_grants (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    principal_type TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    scopes JSONB DEFAULT '[]'::jsonb NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    budget JSONB,
    delegation_required BOOLEAN DEFAULT false NOT NULL,
    CONSTRAINT policy_grants_pkey PRIMARY KEY (id)
);

CREATE INDEX idx_policy_grants_tenant_id ON policy_grants (tenant_id);

CREATE INDEX idx_policy_grants_tenant_principal ON policy_grants (tenant_id, principal_type, principal_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS policy_grants;
-- +goose StatementEnd
