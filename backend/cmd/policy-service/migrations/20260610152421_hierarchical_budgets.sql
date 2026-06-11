-- +goose Up
-- +goose StatementBegin

-- Tenant-level budget cap (one row per tenant). Blocks AGENT/SERVICE
-- principals at the limit; humans accumulate spend but are not blocked.
-- See docs/adr/2026-06-10-hierarchical-budget-enforcement.md.
CREATE TABLE IF NOT EXISTS policy_tenant_budgets (
    tenant_id     TEXT PRIMARY KEY,
    limit_usd     DOUBLE PRECISION NOT NULL,
    reset_cadence TEXT NOT NULL DEFAULT 'monthly',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Rename monthly_limit_usd → limit_usd in the inline budget JSONB now that
-- cadence may be weekly. No data has shipped beyond dev; rewrite in place.
UPDATE policy_grants
SET budget = (budget - 'monthly_limit_usd') || jsonb_build_object('limit_usd', budget->'monthly_limit_usd')
WHERE budget ? 'monthly_limit_usd';

UPDATE policy_role_scopes
SET budget = (budget - 'monthly_limit_usd') || jsonb_build_object('limit_usd', budget->'monthly_limit_usd')
WHERE budget ? 'monthly_limit_usd';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

UPDATE policy_grants
SET budget = (budget - 'limit_usd') || jsonb_build_object('monthly_limit_usd', budget->'limit_usd')
WHERE budget ? 'limit_usd';

UPDATE policy_role_scopes
SET budget = (budget - 'limit_usd') || jsonb_build_object('monthly_limit_usd', budget->'limit_usd')
WHERE budget ? 'limit_usd';

DROP TABLE IF EXISTS policy_tenant_budgets;

-- +goose StatementEnd
