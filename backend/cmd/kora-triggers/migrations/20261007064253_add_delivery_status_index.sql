-- +goose NO TRANSACTION
-- +goose Up
-- Filtering by status across a tenant's full delivery history is the headline
-- case ("show me every failed one"), and idx_trigger_deliveries_tenant only
-- covers (tenant_id, received_at DESC), so a status filter degrades to a scan
-- of every arrival the tenant ever had.
--
-- received_at DESC matches the keyset ORDER BY so one index serves the filter
-- and the page walk together. That only holds while the query says
-- `status = $n`; `status = ANY($n)` is a ScalarArrayOp the generic plan cannot
-- see into, and Postgres falls back to the tenant index with a filter. The
-- single-status case is written as scalar equality for exactly this reason.
-- id is deliberately NOT in the index: it only
-- breaks exact received_at ties, which the existing index already locates
-- cheaply, and this table is write-hot enough that an unnecessary column is a
-- real cost on every insert.
--
-- CONCURRENTLY because this runs as the chart's pre-upgrade hook while the
-- previous orbit-triggers pods are still ingesting webhooks. A plain CREATE
-- INDEX holds a SHARE lock for the whole build, which blocks every INSERT, and
-- inbound webhook ingestion is the one path the platform cannot retry.

-- The DROP is not redundant with IF NOT EXISTS below. An interrupted concurrent
-- build (hook timeout, eviction, OOM) leaves the index present but INVALID, and
-- goose has not recorded the migration, so the Job retries. IF NOT EXISTS would
-- match that invalid index by name, do nothing, and report success, leaving an
-- index the planner never uses but every insert still maintains. Dropping first
-- makes the retry build it for real.
--
-- It is its own StatementBegin block because goose sends a block as one Exec,
-- and two statements in one Exec are an implicit transaction, which both
-- CONCURRENTLY forms refuse: "cannot run inside a transaction block".
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
