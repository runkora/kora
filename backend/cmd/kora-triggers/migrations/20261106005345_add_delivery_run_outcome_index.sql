-- +goose Up
-- Serves the outcome filter, which is an EXISTS over this table. Without it a
-- rare outcome walks the tenant's fan-out rows. delivery_id is third so the
-- selective side can satisfy the semi-join from the index alone.
CREATE INDEX IF NOT EXISTS idx_trigger_delivery_runs_tenant_status
    ON trigger_delivery_runs (tenant_id, status, delivery_id);

-- +goose Down
DROP INDEX IF EXISTS idx_trigger_delivery_runs_tenant_status;
