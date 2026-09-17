-- +goose Up
-- +goose StatementBegin
CREATE TABLE user_pat_tokens (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    jti TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    revoked_at TIMESTAMPTZ,
    scopes JSONB DEFAULT '[]'::jsonb NOT NULL,
    grant_id TEXT,
    CONSTRAINT user_pat_tokens_pkey PRIMARY KEY (id),
    CONSTRAINT user_pat_tokens_jti_key UNIQUE (jti),
    CONSTRAINT user_pat_tokens_token_hash_key UNIQUE (token_hash)
);

CREATE INDEX idx_user_pat_tokens_tenant_id ON user_pat_tokens (tenant_id);

CREATE INDEX idx_user_pat_tokens_user_id ON user_pat_tokens (user_id, tenant_id) WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_pat_tokens;
-- +goose StatementEnd
