-- +goose NO TRANSACTION
-- +goose Up
-- Serves the status filter and the keyset ORDER BY together. id is left out: it
-- only breaks received_at ties, and this table is write-hot.
--
-- See docs/adr/2026-10-07-trigger-deliveries-keyset-pagination.md for why the
-- single-status query uses scalar equality rather than ANY.

-- DROP first: an interrupted CONCURRENTLY build leaves an INVALID index that
-- IF NOT EXISTS would match by name and skip, reporting success.
-- Separate blocks because goose sends one block as a single Exec, and
-- CONCURRENTLY cannot run in the implicit transaction that creates.
-- +goose StatementBegin
DROP INDEX CONCURRENTLY IF EXISTS idx_trigger_deliveries_tenant_status;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_trigger_deliveries_tenant_status
    ON trigger_deliveries (tenant_id, status, received_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX CONCURRENTLY IF EXISTS idx_trigger_deliveries_tenant_status;
-- +goose StatementEnd
