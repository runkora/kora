-- +goose Up
-- +goose StatementBegin
CREATE TABLE trigger_retention_settings (
    tenant_id TEXT NOT NULL,
    payload_days SMALLINT DEFAULT 30 NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT trigger_retention_settings_pkey PRIMARY KEY (tenant_id),
    CONSTRAINT trigger_retention_settings_payload_days_check CHECK (payload_days BETWEEN 1 AND 30)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS trigger_retention_settings;
-- +goose StatementEnd
