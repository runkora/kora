-- +goose Up
-- Provider names are <type> or <type>:<name>, where name is an
-- InferenceProvider resource name. See docs/adr/2026-10-01-crd-inference-providers.md.
-- NOT VALID then VALIDATE avoids holding an exclusive lock while existing rows are checked.
-- +goose StatementBegin
ALTER TABLE virtual_model_routes DROP CONSTRAINT IF EXISTS virtual_model_routes_provider_check;
ALTER TABLE virtual_model_routes ADD CONSTRAINT virtual_model_routes_provider_check
    CHECK (provider ~ '^[a-z]+(:[a-z0-9][-a-z0-9_]*)?$') NOT VALID;
ALTER TABLE virtual_model_routes VALIDATE CONSTRAINT virtual_model_routes_provider_check;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE provider_model_overrides DROP CONSTRAINT IF EXISTS provider_model_overrides_provider_check;
ALTER TABLE provider_model_overrides ADD CONSTRAINT provider_model_overrides_provider_check
    CHECK (provider ~ '^[a-z]+(:[a-z0-9][-a-z0-9_]*)?$') NOT VALID;
ALTER TABLE provider_model_overrides VALIDATE CONSTRAINT provider_model_overrides_provider_check;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE inference_usage DROP CONSTRAINT IF EXISTS inference_usage_provider_check;
ALTER TABLE inference_usage ADD CONSTRAINT inference_usage_provider_check
    CHECK (provider IS NULL OR provider ~ '^[a-z]+(:[a-z0-9][-a-z0-9_]*)?$') NOT VALID;
ALTER TABLE inference_usage VALIDATE CONSTRAINT inference_usage_provider_check;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE virtual_model_routes DROP CONSTRAINT IF EXISTS virtual_model_routes_provider_check;
ALTER TABLE virtual_model_routes ADD CONSTRAINT virtual_model_routes_provider_check
    CHECK (provider IN ('bedrock', 'vertexai', 'azure') OR provider ~ '^bedrock:[a-z][a-z0-9_]*$') NOT VALID;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE provider_model_overrides DROP CONSTRAINT IF EXISTS provider_model_overrides_provider_check;
ALTER TABLE provider_model_overrides ADD CONSTRAINT provider_model_overrides_provider_check
    CHECK (provider IN ('bedrock', 'vertexai', 'azure') OR provider ~ '^bedrock:[a-z][a-z0-9_]*$') NOT VALID;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE inference_usage DROP CONSTRAINT IF EXISTS inference_usage_provider_check;
ALTER TABLE inference_usage ADD CONSTRAINT inference_usage_provider_check
    CHECK (provider IS NULL OR provider IN ('bedrock', 'vertexai', 'azure') OR provider ~ '^bedrock:[a-z][a-z0-9_]*$') NOT VALID;
-- +goose StatementEnd
