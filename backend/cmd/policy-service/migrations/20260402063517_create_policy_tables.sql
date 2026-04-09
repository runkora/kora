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

-- ─── Seed: default scope vocabulary ───────────────────────────────────────────

INSERT INTO policy_scope_vocabulary (scope, resource_type, path_segments, action, description) VALUES
    -- inference
    ('inference:invoke',                 'inference', '[]',                        'invoke', 'Invoke any inference model'),
    ('inference:list',                   'inference', '[]',                        'list',   'List inference models'),
    ('inference:anthropic:invoke',       'inference', '["anthropic"]',             'invoke', 'Invoke Anthropic models'),
    ('inference:openai:invoke',          'inference', '["openai"]',                'invoke', 'Invoke OpenAI models'),
    ('inference:bedrock:invoke',         'inference', '["bedrock"]',               'invoke', 'Invoke AWS Bedrock models'),
    -- mcp
    ('mcp:invoke',                       'mcp',       '[]',                        'invoke', 'Invoke any MCP tool'),
    ('mcp:list',                         'mcp',       '[]',                        'list',   'List MCP servers and tools'),
    ('mcp:gmail:invoke',                 'mcp',       '["gmail"]',                 'invoke', 'Invoke any Gmail MCP tool'),
    ('mcp:gmail:send_email:invoke',      'mcp',       '["gmail","send_email"]',    'invoke', 'Send email via Gmail MCP'),
    ('mcp:gmail:read_email:invoke',      'mcp',       '["gmail","read_email"]',    'invoke', 'Read email via Gmail MCP'),
    ('mcp:msgraph:invoke',               'mcp',       '["msgraph"]',               'invoke', 'Invoke any MS Graph MCP tool'),
    -- external services
    ('external:invoke',                  'external',  '[]',                        'invoke', 'Invoke any external service'),
    ('external:msgraph:invoke',          'external',  '["msgraph"]',               'invoke', 'Invoke MS Graph'),
    ('external:msgraph:mail:read',       'external',  '["msgraph","mail"]',        'read',   'Read MS Graph mail'),
    ('external:msgraph:mail:write',      'external',  '["msgraph","mail"]',        'write',  'Write MS Graph mail'),
    -- platform management
    ('platform:grants:read',             'platform',  '["grants"]',                'read',   'Read policy grants'),
    ('platform:grants:write',            'platform',  '["grants"]',                'write',  'Create and delete policy grants'),
    ('platform:agents:read',             'platform',  '["agents"]',                'read',   'Read agent registrations'),
    ('platform:tenants:manage',          'platform',  '["tenants"]',               'manage', 'Manage tenants (platform-admin only)')
ON CONFLICT (scope) DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_policy_scope_vocabulary_resource_type;
DROP INDEX IF EXISTS idx_policy_grants_tenant_id;
DROP INDEX IF EXISTS idx_policy_grants_tenant_principal;

DROP TABLE IF EXISTS policy_scope_vocabulary;
DROP TABLE IF EXISTS policy_grants;

-- +goose StatementEnd
