-- +goose Up
-- +goose StatementBegin
ALTER TABLE virtual_model_routes
    DROP CONSTRAINT virtual_model_routes_provider_check,
    ADD CONSTRAINT virtual_model_routes_provider_check
        CHECK (
            provider IN ('bedrock', 'vertexai', 'azure')
            OR provider ~ '^bedrock:[a-z][a-z0-9_-]*$'
        );
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE inference_usage
    DROP CONSTRAINT inference_usage_provider_check,
    ADD CONSTRAINT inference_usage_provider_check
        CHECK (
            provider IS NULL
            OR provider IN ('bedrock', 'vertexai', 'azure')
            OR provider ~ '^bedrock:[a-z][a-z0-9_-]*$'
        );
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE provider_model_overrides
    DROP CONSTRAINT provider_model_overrides_provider_check,
    ADD CONSTRAINT provider_model_overrides_provider_check
        CHECK (
            provider IN ('bedrock', 'vertexai', 'azure')
            OR provider ~ '^bedrock:[a-z][a-z0-9_-]*$'
        );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE virtual_model_routes
    DROP CONSTRAINT virtual_model_routes_provider_check,
    ADD CONSTRAINT virtual_model_routes_provider_check
        CHECK (provider = ANY (ARRAY['bedrock'::text, 'vertexai'::text, 'azure'::text]));
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE inference_usage
    DROP CONSTRAINT inference_usage_provider_check,
    ADD CONSTRAINT inference_usage_provider_check
        CHECK ((provider = ANY (ARRAY['bedrock'::text, 'vertexai'::text, 'azure'::text])) OR provider IS NULL);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE provider_model_overrides
    DROP CONSTRAINT provider_model_overrides_provider_check,
    ADD CONSTRAINT provider_model_overrides_provider_check
        CHECK (provider IN ('bedrock', 'vertexai', 'azure'));
-- +goose StatementEnd
