-- +goose Up
-- +goose StatementBegin
-- Drop old three-table quota model if present from any partial prior run.
DROP TABLE IF EXISTS principal_quota_role_memberships;
DROP TABLE IF EXISTS quota_roles;
DROP TABLE IF EXISTS principal_quotas;

-- Add inline budget JSONB to the existing role and grant tables.
-- NULL means "no budget configured for this category".
-- Non-null shape: {"monthly_limit_usd": 50.00, "reset_cadence": "monthly"}
ALTER TABLE policy_role_scopes ADD COLUMN IF NOT EXISTS budget JSONB NULL;
ALTER TABLE policy_grants ADD COLUMN IF NOT EXISTS budget JSONB NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE policy_grants DROP COLUMN IF EXISTS budget;
ALTER TABLE policy_role_scopes DROP COLUMN IF EXISTS budget;
-- +goose StatementEnd
