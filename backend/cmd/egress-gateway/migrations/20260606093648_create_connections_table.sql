-- +goose Up
-- +goose StatementBegin

-- ─── connections ──────────────────────────────────────────────────────────────
-- One row per registered third-party connection per tenant. The egress gateway
-- resolves an outbound call to a connection by (tenant_id, name) and uses the
-- adapter to mint or resolve the credential just-in-time. The agent never sees
-- the credential; this table holds only references to per-tenant secret material.
--
-- adapter is intentionally an unconstrained TEXT: adapter types are an open
-- registry of pluggable code (see internal/credentials/adapters/), not a closed
-- state machine. Validation happens at the application layer — a connection
-- naming an unregistered adapter fails at resolve time with ErrUnsupportedAdapter.
-- A CHECK constraint here would force a database migration for every new
-- connector and still couldn't catch the things validation actually needs to.

CREATE TABLE IF NOT EXISTS connections (
    id            TEXT        PRIMARY KEY,
    tenant_id     TEXT        NOT NULL,
    name          TEXT        NOT NULL,
    adapter       TEXT        NOT NULL,
    base_host     TEXT        NOT NULL,
    secret_ref    TEXT        NOT NULL,
    scope_ceiling JSONB       NOT NULL DEFAULT '[]'::jsonb,
    config        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_connections_tenant_id
    ON connections (tenant_id, name);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS connections;

-- +goose StatementEnd
