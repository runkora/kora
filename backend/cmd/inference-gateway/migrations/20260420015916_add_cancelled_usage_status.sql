-- +goose Up
-- +goose StatementBegin
ALTER TABLE inference_usage
    DROP CONSTRAINT IF EXISTS inference_usage_status_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_status_check
    CHECK (status IN ('success', 'provider_error', 'policy_denied', 'auth_error', 'timeout', 'routing_failed', 'canceled'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE inference_usage
    DROP CONSTRAINT IF EXISTS inference_usage_status_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_status_check
    CHECK (status IN ('success', 'provider_error', 'policy_denied', 'auth_error', 'timeout', 'routing_failed'));
-- +goose StatementEnd
