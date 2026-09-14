-- +goose Up
-- +goose StatementBegin
CREATE TABLE policy_tenant_budgets (
    tenant_id TEXT NOT NULL,
    limit_usd DOUBLE PRECISION NOT NULL,
    reset_cadence TEXT DEFAULT 'monthly' NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT policy_tenant_budgets_pkey PRIMARY KEY (tenant_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS policy_tenant_budgets;
-- +goose StatementEnd
