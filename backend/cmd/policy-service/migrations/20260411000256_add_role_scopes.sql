-- +goose Up
-- +goose StatementBegin

-- ─── policy_role_scopes ───────────────────────────────────────────────────────
-- Maps OIDC role names to platform scopes within a tenant. Users assigned a
-- role in the OIDC provider automatically receive the scopes mapped here —
-- no per-user grant is required. Platform admin roles (tenant-admin,
-- platform-admin) are hardcoded in the bundle compiler and are NOT stored here.
--

-- Upsert semantics: the API replaces the full scopes list for a given role name.
-- scopes is stored as JSONB (array of scope strings).

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

-- ─── Seed: platform:roles vocabulary entries ──────────────────────────────────

INSERT INTO policy_scope_vocabulary (scope, resource_type, path_segments, action, description) VALUES
    ('platform:roles:read',  'platform', '["roles"]', 'read',  'Read role→scope mappings'),
    ('platform:roles:write', 'platform', '["roles"]', 'write', 'Create and delete role→scope mappings')
ON CONFLICT (scope) DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM policy_scope_vocabulary WHERE scope IN ('platform:roles:read', 'platform:roles:write');

DROP INDEX IF EXISTS idx_policy_role_scopes_tenant_id;
DROP TABLE IF EXISTS policy_role_scopes;

-- +goose StatementEnd
