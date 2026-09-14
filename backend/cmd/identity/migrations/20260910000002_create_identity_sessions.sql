-- +goose Up
-- +goose StatementBegin
CREATE TABLE identity_sessions (
    id TEXT NOT NULL,
    jti TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    principal_type TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    display_name TEXT DEFAULT '' NOT NULL,
    identity_source TEXT NOT NULL,
    claims JSONB DEFAULT '{}'::jsonb NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT identity_sessions_pkey PRIMARY KEY (id),
    CONSTRAINT identity_sessions_jti_key UNIQUE (jti)
);

CREATE INDEX idx_identity_sessions_expires_at ON identity_sessions (expires_at) WHERE (revoked_at IS NULL);

CREATE INDEX idx_identity_sessions_principal_id ON identity_sessions (principal_id, tenant_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS identity_sessions;
-- +goose StatementEnd
