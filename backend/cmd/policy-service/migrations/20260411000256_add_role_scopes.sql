-- +goose Up
-- +goose StatementBegin

-- ─── Seed: platform:roles vocabulary entries ──────────────────────────────────
-- Ensures the policy:roles:read and policy:roles:write scopes are available in
-- the vocabulary for grant validation. The policy_role_scopes table itself is
-- created in the base migration (20260402063517).

INSERT INTO policy_scope_vocabulary (scope, resource_type, path_segments, action, description) VALUES
    ('platform:roles:read',  'platform', '["roles"]', 'read',  'Read role→scope mappings'),
    ('platform:roles:write', 'platform', '["roles"]', 'write', 'Create and delete role→scope mappings')
ON CONFLICT (scope) DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM policy_scope_vocabulary WHERE scope IN ('platform:roles:read', 'platform:roles:write');

-- +goose StatementEnd
