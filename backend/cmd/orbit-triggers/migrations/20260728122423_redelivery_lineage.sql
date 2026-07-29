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

-- Rolling back re-imposes the stricter uniqueness, which the rows written since
-- the Up migration legitimately violate: every provider retry and every
-- operator replay is a second row carrying the original's provider delivery id.
-- Recreating the index over them fails, and it fails half-way through the Down
-- block — after the lineage index is already gone, leaving the schema in
-- neither shape.
--
-- The lineage rows go first. They are exactly the rows the old schema could not
-- represent, so a rollback to it is a decision to give them up; their originals,
-- which are what the outcome history hangs off, are untouched. Deleting them is
-- the only way this Down block can complete on real data.
DELETE FROM trigger_delivery_runs
WHERE  delivery_id IN (SELECT id FROM trigger_deliveries WHERE redelivery_of IS NOT NULL);

DELETE FROM trigger_pending_deliveries
WHERE  delivery_id IN (SELECT id FROM trigger_deliveries WHERE redelivery_of IS NOT NULL);

DELETE FROM trigger_deliveries WHERE redelivery_of IS NOT NULL;

DROP INDEX IF EXISTS idx_trigger_deliveries_redelivery_of;
DROP INDEX IF EXISTS idx_trigger_deliveries_dedup;

CREATE UNIQUE INDEX idx_trigger_deliveries_dedup
    ON trigger_deliveries (source_id, provider_delivery_id)
    WHERE provider_delivery_id <> '';

-- +goose StatementEnd
