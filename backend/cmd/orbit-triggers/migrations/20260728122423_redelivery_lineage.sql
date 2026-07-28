-- +goose Up
-- +goose StatementBegin

-- Redelivery lineage: a repeat arrival becomes a row, not silence.
--
-- The dedup index made "already handled" unrepresentable. A provider retry, and
-- an operator-requested replay, both carry the original's provider delivery id,
-- so inserting either one violated the unique index and the arrival left no
-- trace at all.
--
-- The guarantee that matters is unchanged: at most one ORIGINAL delivery per
-- (source_id, provider_delivery_id). Rows that name the delivery they descend
-- from (redelivery_of IS NOT NULL) are lineage, not originals, so they are
-- excluded from the uniqueness constraint rather than blocked by it.
DROP INDEX IF EXISTS idx_trigger_deliveries_dedup;

CREATE UNIQUE INDEX idx_trigger_deliveries_dedup
    ON trigger_deliveries (source_id, provider_delivery_id)
    WHERE provider_delivery_id <> '' AND redelivery_of IS NULL;

-- Reading a delivery's lineage ("what replayed this?") is a supported query on
-- the delivery history, so it gets an index rather than a sequential scan.
CREATE INDEX idx_trigger_deliveries_redelivery_of
    ON trigger_deliveries (redelivery_of)
    WHERE redelivery_of IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_trigger_deliveries_redelivery_of;
DROP INDEX IF EXISTS idx_trigger_deliveries_dedup;

CREATE UNIQUE INDEX idx_trigger_deliveries_dedup
    ON trigger_deliveries (source_id, provider_delivery_id)
    WHERE provider_delivery_id <> '';

-- +goose StatementEnd
