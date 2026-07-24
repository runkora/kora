-- +goose Up
-- +goose StatementBegin
ALTER TABLE virtual_model_routes
    DROP CONSTRAINT virtual_model_routes_provider_check,
    ADD CONSTRAINT virtual_model_routes_provider_check
        CHECK (
            provider IN ('bedrock', 'vertexai', 'azure')
            OR provider ~ '^bedrock:[a-z][a-z0-9_]*$'
        );
-- +goose StatementEnd

-- +goose Down
-- NOTE: safe to roll back only when no bedrock:<alias> rows exist in this table.
-- If aliased rows are present, ADD CONSTRAINT will fail with a constraint violation.
-- +goose StatementBegin
ALTER TABLE virtual_model_routes
    DROP CONSTRAINT virtual_model_routes_provider_check,
    ADD CONSTRAINT virtual_model_routes_provider_check
        CHECK (provider = ANY (ARRAY['bedrock'::text, 'vertexai'::text, 'azure'::text]));
-- +goose StatementEnd
