-- +goose Up
-- +goose StatementBegin
ALTER TABLE inference_usage
    DROP CONSTRAINT inference_usage_provider_check,
    ADD CONSTRAINT inference_usage_provider_check
        CHECK (
            provider IS NULL
            OR provider IN ('bedrock', 'vertexai', 'azure')
            OR provider ~ '^bedrock:[a-z][a-z0-9_]*$'
        );
-- +goose StatementEnd

-- +goose Down
-- NOTE: safe to roll back only when no bedrock:<alias> rows exist in this table.
-- If aliased rows are present, ADD CONSTRAINT will fail with a constraint violation.
-- +goose StatementBegin
ALTER TABLE inference_usage
    DROP CONSTRAINT inference_usage_provider_check,
    ADD CONSTRAINT inference_usage_provider_check
        CHECK ((provider = ANY (ARRAY['bedrock'::text, 'vertexai'::text, 'azure'::text])) OR provider IS NULL);
-- +goose StatementEnd
