-- +goose Up
-- Serves the status filter and the keyset ORDER BY together. id is left out: it
-- only breaks received_at ties, and this table is write-hot.
--
-- See docs/adr/2026-10-07-trigger-deliveries-keyset-pagination.md for why the
-- single-status query uses scalar equality rather than ANY.
CREATE INDEX IF NOT EXISTS idx_trigger_deliveries_tenant_status
    ON trigger_deliveries (tenant_id, status, received_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_trigger_deliveries_tenant_status;
