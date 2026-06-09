-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS quota_groups (
    id                  TEXT        NOT NULL,
    tenant_id           TEXT        NOT NULL,
    name                TEXT        NOT NULL,
    monthly_budget_usd  NUMERIC(14, 6) NOT NULL CHECK (monthly_budget_usd > 0),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS principal_quota_group_memberships (
    principal_id    TEXT        NOT NULL,
    tenant_id       TEXT        NOT NULL,
    group_id        TEXT        NOT NULL REFERENCES quota_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (principal_id, tenant_id)
);

CREATE TABLE IF NOT EXISTS principal_quotas (
    principal_id        TEXT        NOT NULL,
    tenant_id           TEXT        NOT NULL,
    monthly_budget_usd  NUMERIC(14, 6) NOT NULL CHECK (monthly_budget_usd > 0),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_quota_groups_tenant_id
    ON quota_groups (tenant_id);

CREATE INDEX IF NOT EXISTS idx_principal_quota_memberships_group_id
    ON principal_quota_group_memberships (group_id);

CREATE INDEX IF NOT EXISTS idx_principal_quotas_tenant_id
    ON principal_quotas (tenant_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_principal_quotas_tenant_id;
DROP INDEX IF EXISTS idx_principal_quota_memberships_group_id;
DROP INDEX IF EXISTS idx_quota_groups_tenant_id;
DROP TABLE IF EXISTS principal_quotas;
DROP TABLE IF EXISTS principal_quota_group_memberships;
DROP TABLE IF EXISTS quota_groups;
-- +goose StatementEnd
