-- +goose Up
-- +goose StatementBegin

-- ─── identity_sessions ────────────────────────────────────────────────────────
-- One row per issued platform session token.
-- claims is stored as JSONB so provider-specific attributes can vary by IdP
-- without requiring a schema migration.

CREATE TABLE IF NOT EXISTS identity_sessions (
    id              TEXT        PRIMARY KEY,
    jti             TEXT        NOT NULL UNIQUE,            -- JWT ID — also used as blocklist key
    principal_id    TEXT        NOT NULL,                   -- upstream IdP sub or agent registry ID
    principal_type  TEXT        NOT NULL,                   -- HUMAN | AGENT | SERVICE | SYSTEM
    tenant_id       TEXT        NOT NULL,
    display_name    TEXT        NOT NULL DEFAULT '',
    identity_source TEXT        NOT NULL,                   -- FEDERATED | ATTESTED | PLATFORM
    claims          JSONB       NOT NULL DEFAULT '{}',      -- provider-specific verified attributes
    created_at      TIMESTAMPTZ NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ                             -- NULL = active
);

-- idx_identity_sessions_jti is intentionally absent: the UNIQUE constraint on
-- jti already creates a B-tree index. A redundant explicit index was removed.

CREATE INDEX IF NOT EXISTS idx_identity_sessions_principal_id
    ON identity_sessions (principal_id, tenant_id);

CREATE INDEX IF NOT EXISTS idx_identity_sessions_expires_at
    ON identity_sessions (expires_at)
    WHERE revoked_at IS NULL;

-- ─── identity_refresh_tokens ──────────────────────────────────────────────────
-- Only the SHA-256 hash of the token is stored — the raw value is returned to
-- the client exactly once and never persisted. This limits exposure if the
-- table is compromised.

CREATE TABLE IF NOT EXISTS identity_refresh_tokens (
    id          TEXT        PRIMARY KEY,
    session_id  TEXT        NOT NULL REFERENCES identity_sessions (id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL UNIQUE,                -- SHA-256 hex of the raw token
    created_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ                                 -- NULL = active
);

CREATE INDEX IF NOT EXISTS idx_identity_refresh_tokens_session_id
    ON identity_refresh_tokens (session_id);

CREATE INDEX IF NOT EXISTS idx_identity_refresh_tokens_expires_at
    ON identity_refresh_tokens (expires_at)
    WHERE revoked_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_identity_refresh_tokens_expires_at;
DROP INDEX IF EXISTS idx_identity_refresh_tokens_session_id;
DROP TABLE IF EXISTS identity_refresh_tokens;

DROP INDEX IF EXISTS idx_identity_sessions_expires_at;
DROP INDEX IF EXISTS idx_identity_sessions_principal_id;
DROP TABLE IF EXISTS identity_sessions;

-- +goose StatementEnd
