-- +goose Up
-- +goose StatementBegin
CREATE TABLE environments (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    egress_hosts TEXT[] DEFAULT '{}' NOT NULL,
    connectors JSONB DEFAULT '[]'::jsonb NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT environments_pkey PRIMARY KEY (id),
    CONSTRAINT environments_tenant_id_name_key UNIQUE (tenant_id, name)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS environments;
-- +goose StatementEnd
