-- +goose Up
-- +goose StatementBegin

-- Index to support future queries that filter provider_model_overrides by
-- provider alone (e.g. "list all disabled models for provider X"), which would
-- otherwise perform a full table scan against the composite PK.
CREATE INDEX IF NOT EXISTS idx_provider_model_overrides_provider
    ON provider_model_overrides (provider);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_provider_model_overrides_provider;

-- +goose StatementEnd
