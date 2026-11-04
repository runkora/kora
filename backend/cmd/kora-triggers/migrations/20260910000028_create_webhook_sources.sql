-- +goose Up
-- +goose StatementBegin
CREATE TABLE webhook_sources (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    grant_id TEXT,
    secret_ciphertext TEXT NOT NULL,
    config JSONB DEFAULT '{}'::jsonb NOT NULL,
    enabled BOOLEAN DEFAULT true NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    rejection_count BIGINT DEFAULT 0 NOT NULL,
    last_rejection_at TIMESTAMPTZ,
    last_rejection_reason TEXT,
    secret_rotated_at TIMESTAMPTZ,
    accepted_count BIGINT DEFAULT 0 NOT NULL,
    CONSTRAINT webhook_sources_pkey PRIMARY KEY (id),
    CONSTRAINT webhook_sources_tenant_id_name_key UNIQUE (tenant_id, name)
);

CREATE INDEX idx_webhook_sources_tenant_id ON webhook_sources (tenant_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS webhook_sources;
-- +goose StatementEnd
