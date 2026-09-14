-- +goose Up
-- +goose StatementBegin
CREATE TABLE identity_refresh_tokens (
    id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT identity_refresh_tokens_pkey PRIMARY KEY (id),
    CONSTRAINT identity_refresh_tokens_token_hash_key UNIQUE (token_hash),
    CONSTRAINT identity_refresh_tokens_session_id_fkey FOREIGN KEY (session_id) REFERENCES identity_sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_identity_refresh_tokens_expires_at ON identity_refresh_tokens (expires_at) WHERE revoked_at IS NULL;

CREATE INDEX idx_identity_refresh_tokens_session_id ON identity_refresh_tokens (session_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS identity_refresh_tokens;
-- +goose StatementEnd
