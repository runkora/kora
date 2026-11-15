-- +goose Up
-- The platform event an event delivery fired on. One original delivery per event.
ALTER TABLE trigger_deliveries ADD COLUMN event_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_trigger_deliveries_event
    ON trigger_deliveries (event_id) WHERE redelivery_of IS NULL;

-- The seq of the last event the event consumer handled.
CREATE TABLE IF NOT EXISTS trigger_event_cursor (
    id       BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    last_seq BIGINT NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE IF EXISTS trigger_event_cursor;
DROP INDEX IF EXISTS idx_trigger_deliveries_event;
ALTER TABLE trigger_deliveries DROP COLUMN IF EXISTS event_id;
