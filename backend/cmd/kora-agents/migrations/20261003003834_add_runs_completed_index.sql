-- +goose Up
-- +goose StatementBegin
-- Serves the Agents page run outcome counts: finished runs of a tenant in a time window.
CREATE INDEX idx_runs_tenant_completed ON runs (tenant_id, completed_at DESC) WHERE completed_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_runs_tenant_completed;
-- +goose StatementEnd
