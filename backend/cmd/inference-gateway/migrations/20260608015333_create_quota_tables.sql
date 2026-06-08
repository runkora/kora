-- +goose Up
-- +goose StatementBegin
CREATE TABLE quota_groups (
    id                 TEXT           NOT NULL PRIMARY KEY,
    tenant_id          TEXT           NOT NULL,
    name               TEXT           NOT NULL,
    monthly_budget_usd NUMERIC(12, 4) NOT NULL,
    created_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE principal_quota_group_memberships (
    principal_id TEXT        NOT NULL,
    tenant_id    TEXT        NOT NULL,
    group_id     TEXT        NOT NULL REFERENCES quota_groups(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, tenant_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE principal_quotas (
    principal_id       TEXT           NOT NULL,
    tenant_id          TEXT           NOT NULL,
    monthly_budget_usd NUMERIC(12, 4) NOT NULL,
    created_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, tenant_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS principal_quotas;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS principal_quota_group_memberships;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS quota_groups;
-- +goose StatementEnd
