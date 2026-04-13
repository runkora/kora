-- +goose Up
-- +goose StatementBegin

-- ─── policy_grants ────────────────────────────────────────────────────────────
-- A grant asserts that a named principal within a tenant holds a set of
-- OAuth-style permission scopes. Default posture is deny — access is allowed
-- if and only if a matching grant exists.
--
-- scopes is stored as JSONB (array of scope strings).
-- No update operation — modifications are expressed as delete + create.

CREATE TABLE IF NOT EXISTS policy_grants (
    id             TEXT        PRIMARY KEY,
    tenant_id      TEXT        NOT NULL,
    principal_type TEXT        NOT NULL,   -- AGENT | HUMAN | SERVICE
    principal_id   TEXT        NOT NULL,
    scopes         JSONB       NOT NULL DEFAULT '[]',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_policy_grants_tenant_principal
    ON policy_grants (tenant_id, principal_type, principal_id);

CREATE INDEX IF NOT EXISTS idx_policy_grants_tenant_id
    ON policy_grants (tenant_id);

-- ─── policy_scope_vocabulary ──────────────────────────────────────────────────
-- The finite registered set of valid OAuth scope strings and their hierarchy
-- metadata. Used for grant validation at write time — not for runtime hierarchy
-- resolution (which uses structural string parsing in Rego).

CREATE TABLE IF NOT EXISTS policy_scope_vocabulary (
    scope         TEXT        PRIMARY KEY,
    resource_type TEXT        NOT NULL,
    path_segments JSONB       NOT NULL DEFAULT '[]',  -- ["anthropic", "claude"]
    action        TEXT        NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_policy_scope_vocabulary_resource_type
    ON policy_scope_vocabulary (resource_type);

-- ─── policy_role_scopes ───────────────────────────────────────────────────────
-- Maps an OIDC role name within a tenant to a set of platform scopes.
-- Users carrying a matching role in their JWT are granted those scopes
-- without requiring a per-user grant entry.

CREATE TABLE IF NOT EXISTS policy_role_scopes (
    id         TEXT        PRIMARY KEY,
    tenant_id  TEXT        NOT NULL,
    role       TEXT        NOT NULL,
    scopes     JSONB       NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, role)
);

CREATE INDEX IF NOT EXISTS idx_policy_role_scopes_tenant_id
    ON policy_role_scopes (tenant_id);


-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_policy_role_scopes_tenant_id;
DROP INDEX IF EXISTS idx_policy_scope_vocabulary_resource_type;
DROP INDEX IF EXISTS idx_policy_grants_tenant_id;
DROP INDEX IF EXISTS idx_policy_grants_tenant_principal;

DROP TABLE IF EXISTS policy_role_scopes;
DROP TABLE IF EXISTS policy_scope_vocabulary;
DROP TABLE IF EXISTS policy_grants;

-- +goose StatementEnd
