-- +goose Up
-- +goose StatementBegin
-- A crd row is projected from a PolicyRole resource, named by resource_name and resource_uid.
-- repository is the resource's runkora.io/repository annotation, shown on the row.
ALTER TABLE policy_role_scopes
    ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
    ADD COLUMN resource_uid TEXT,
    ADD COLUMN resource_name TEXT,
    ADD COLUMN repository TEXT;

-- A crd tenant budget comes from the KoraTenant budget in the tenant registry. It has no resource of its own.
ALTER TABLE policy_tenant_budgets
    ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE policy_tenant_budgets DROP COLUMN IF EXISTS source;
ALTER TABLE policy_role_scopes
    DROP COLUMN IF EXISTS repository,
    DROP COLUMN IF EXISTS resource_name,
    DROP COLUMN IF EXISTS resource_uid,
    DROP COLUMN IF EXISTS source;
-- +goose StatementEnd
