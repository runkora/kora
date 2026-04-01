-- +goose Up
-- +goose StatementBegin

-- ─── user_pat_tokens ──────────────────────────────────────────────────────────
-- Stores metadata and a SHA-256 hash for each Personal Access Token (PAT).
-- Raw token values are never persisted — the caller receives the raw JWT once
-- at creation time and is responsible for storing it. This limits exposure if
-- the table is compromised.
--
-- PATs are user-bound (user_id = principal_id of the issuing HUMAN principal)
-- and tenant-scoped. They carry PrincipalType=PAT in their JWT claims and are
-- accepted only at inference endpoints (see ADR-0033).
--
-- Revocation uses the existing JTI blocklist (Redis) keyed on the jti column.
-- The revoked_at column provides a durable audit record and ensures LIST
-- queries can filter revoked tokens without a Redis round-trip.

CREATE TABLE user_pat_tokens (
    id          TEXT        PRIMARY KEY,
    tenant_id   TEXT        NOT NULL,
    user_id     TEXT        NOT NULL,        -- principal_id of the issuing HUMAN principal
    name        TEXT        NOT NULL,        -- user-assigned label (1–128 chars)
    token_hash  TEXT        NOT NULL UNIQUE, -- SHA-256 hex of the raw JWT; never queried for auth,
                                            -- stored as a security property (no raw tokens at rest)
    jti         TEXT        NOT NULL UNIQUE, -- JWT ID claim; used as the JTI blocklist key on revocation
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at  TIMESTAMPTZ                 -- NULL = active
);

CREATE INDEX idx_user_pat_tokens_user_id
    ON user_pat_tokens (user_id, tenant_id)
    WHERE revoked_at IS NULL;

CREATE INDEX idx_user_pat_tokens_tenant_id
    ON user_pat_tokens (tenant_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_user_pat_tokens_tenant_id;
DROP INDEX IF EXISTS idx_user_pat_tokens_user_id;
DROP TABLE IF EXISTS user_pat_tokens;

-- +goose StatementEnd
