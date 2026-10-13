-- +goose NO TRANSACTION
-- +goose Up
-- Serves the outcome filter, which is an EXISTS over this table. Without it a
-- rare outcome walks the tenant's fan-out rows. delivery_id is third so the
-- selective side can satisfy the semi-join from the index alone.

-- DROP first: an interrupted CONCURRENTLY build leaves an INVALID index that
-- IF NOT EXISTS would match by name and skip, reporting success.
-- Separate blocks because goose sends one block as a single Exec, and
-- CONCURRENTLY cannot run in the implicit transaction that creates.
-- +goose StatementBegin
DROP INDEX CONCURRENTLY IF EXISTS idx_trigger_delivery_runs_tenant_status;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_trigger_delivery_runs_tenant_status
    ON trigger_delivery_runs (tenant_id, status, delivery_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX CONCURRENTLY IF EXISTS idx_trigger_delivery_runs_tenant_status;
-- +goose StatementEnd
