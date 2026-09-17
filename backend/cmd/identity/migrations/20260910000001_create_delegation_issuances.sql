-- +goose Up
-- +goose StatementBegin
CREATE TABLE delegation_issuances (
    jti TEXT NOT NULL,
    run_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    delegator_principal_id TEXT NOT NULL,
    delegator_principal_type TEXT NOT NULL,
    delegator_pat_id TEXT,
    agent_principal_id TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT delegation_issuances_pkey PRIMARY KEY (jti)
);

CREATE UNIQUE INDEX delegation_issuances_active_run_idx ON delegation_issuances (run_id) WHERE revoked_at IS NULL;

CREATE INDEX delegation_issuances_delegator_idx ON delegation_issuances (tenant_id, delegator_principal_id) WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS delegation_issuances;
-- +goose StatementEnd
