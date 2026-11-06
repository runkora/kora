-- +goose Up
-- The delivery's received_at, copied onto each outcome row so a trigger's
-- deliveries, its last fire and the 24h stats read in received_at order from
-- one index. The default covers rows written by pods older than this column.
ALTER TABLE trigger_delivery_runs ADD COLUMN received_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE trigger_delivery_runs dr
SET    received_at = d.received_at
FROM   trigger_deliveries d
WHERE  d.id = dr.delivery_id;

-- A trigger's deliveries page, unfiltered and by outcome.
CREATE INDEX IF NOT EXISTS idx_trigger_delivery_runs_reach
    ON trigger_delivery_runs (tenant_id, trigger_id, received_at DESC, delivery_id DESC);
CREATE INDEX IF NOT EXISTS idx_trigger_delivery_runs_reach_status
    ON trigger_delivery_runs (tenant_id, trigger_id, status, received_at DESC, delivery_id DESC);

-- A trigger's last fire skips declined outcomes without reading them.
CREATE INDEX IF NOT EXISTS idx_trigger_delivery_runs_last_fired
    ON trigger_delivery_runs (tenant_id, trigger_id, received_at DESC)
    WHERE status IN ('dispatched', 'joined', 'failed', 'denied');

-- Deliveries recorded against a trigger that carry no outcome of their own.
CREATE INDEX IF NOT EXISTS idx_trigger_deliveries_recorded
    ON trigger_deliveries (tenant_id, trigger_id, status, received_at DESC, id DESC)
    WHERE status IN ('verified', 'missed', 'duplicate');

-- +goose Down
DROP INDEX IF EXISTS idx_trigger_deliveries_recorded;
DROP INDEX IF EXISTS idx_trigger_delivery_runs_last_fired;
DROP INDEX IF EXISTS idx_trigger_delivery_runs_reach_status;
DROP INDEX IF EXISTS idx_trigger_delivery_runs_reach;
ALTER TABLE trigger_delivery_runs DROP COLUMN IF EXISTS received_at;
