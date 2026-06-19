-- +goose Up
-- +goose StatementBegin
-- trigger_delivery_runs records the full fan-out of a single inbound delivery: a
-- delivery may match multiple triggers and start a run for each, so the
-- one-to-many (delivery → trigger → run) detail lives here. trigger_deliveries
-- keeps a delivery-level summary (overall status + the first dispatched run);
-- this table is the authoritative, complete audit record.
--
-- delivery_id is FK'd with ON DELETE CASCADE because a run record has no meaning
-- without its delivery. trigger_id is intentionally NOT foreign-keyed so the
-- history survives a trigger deletion (mirroring trigger_deliveries).
CREATE TABLE trigger_delivery_runs (
    id            TEXT        PRIMARY KEY,
    tenant_id     TEXT        NOT NULL,
    delivery_id   TEXT        NOT NULL REFERENCES trigger_deliveries (id) ON DELETE CASCADE,
    trigger_id    TEXT        NOT NULL,                 -- matched trigger (no FK: retained after trigger deletion)
    run_id        TEXT,                                 -- started run (null when dispatch failed)
    status        TEXT        NOT NULL,                 -- dispatched|failed
    error         TEXT,                                 -- dispatch failure detail (null on success)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_trigger_delivery_runs_delivery ON trigger_delivery_runs (delivery_id);
CREATE INDEX idx_trigger_delivery_runs_trigger ON trigger_delivery_runs (tenant_id, trigger_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS trigger_delivery_runs;
-- +goose StatementEnd
