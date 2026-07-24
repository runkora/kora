-- +goose Up
-- +goose StatementBegin
-- Sync inference_usage_status_check with the full inferencegw.UsageStatus enum.
-- Two values the service writes were absent from the constraint, so those rows
-- were rejected with SQLSTATE 23514 (the request's HTTP response was still
-- correct, but the usage/telemetry row was dropped):
--   * model_not_found  -- added in #136 for unknown-model requests
--   * budget_exceeded  -- pre-existing gap; writeFailedUsage(..., UsageStatusBudgetExceeded, ...)
ALTER TABLE inference_usage
    DROP CONSTRAINT IF EXISTS inference_usage_status_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_status_check
    CHECK (status IN ('success', 'provider_error', 'policy_denied', 'model_not_found', 'budget_exceeded', 'auth_error', 'timeout', 'routing_failed', 'canceled'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Faithful revert to the pre-migration (add_cancelled) constraint. Rolling back
-- reintroduces the model_not_found and budget_exceeded 23514 gaps.
ALTER TABLE inference_usage
    DROP CONSTRAINT IF EXISTS inference_usage_status_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_status_check
    CHECK (status IN ('success', 'provider_error', 'policy_denied', 'auth_error', 'timeout', 'routing_failed', 'canceled'));
-- +goose StatementEnd
