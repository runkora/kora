-- +goose Up
-- +goose StatementBegin
CREATE TABLE provider_model_overrides (
    provider TEXT NOT NULL,
    model_id TEXT NOT NULL,
    enabled BOOLEAN DEFAULT true NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT provider_model_overrides_pkey PRIMARY KEY (provider, model_id),
    CONSTRAINT provider_model_overrides_provider_check CHECK ((provider IN ('bedrock', 'vertexai', 'azure')) OR (provider ~ '^bedrock:[a-z][a-z0-9_]*$'))
);

CREATE INDEX idx_provider_model_overrides_provider ON provider_model_overrides (provider);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS provider_model_overrides;
-- +goose StatementEnd
