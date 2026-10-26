-- +goose Up
-- +goose StatementBegin
-- A crd row is projected from a PolicyRole resource, named by resource_name and resource_uid.
-- repository is the resource's runkora.io/repository annotation, shown on the row.
ALTER TABLE policy_role_scopes
    ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
    ADD COLUMN resource_uid TEXT,
    ADD COLUMN resource_name TEXT,
    ADD COLUMN repository TEXT,
    ADD CONSTRAINT policy_role_scopes_resource_check
        CHECK (source = 'api' OR (resource_name IS NOT NULL AND resource_uid IS NOT NULL));
-- One PolicyRole projects at most one row in its tenant.
CREATE UNIQUE INDEX policy_role_scopes_resource_name_key ON policy_role_scopes (tenant_id, resource_name) WHERE source = 'crd';

-- A crd tenant budget comes from the KoraTenant budget in the tenant registry. It has no resource of its own.
ALTER TABLE policy_tenant_budgets
    ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE policy_tenant_budgets DROP COLUMN IF EXISTS source;
DROP INDEX IF EXISTS policy_role_scopes_resource_name_key;
ALTER TABLE policy_role_scopes
    DROP CONSTRAINT IF EXISTS policy_role_scopes_resource_check,
    DROP COLUMN IF EXISTS repository,
    DROP COLUMN IF EXISTS resource_name,
    DROP COLUMN IF EXISTS resource_uid,
    DROP COLUMN IF EXISTS source;
-- +goose StatementEnd
