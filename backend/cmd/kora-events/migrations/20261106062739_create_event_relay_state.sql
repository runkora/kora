-- +goose Up
-- The relay's single row: the last seq it assigned. kora-events inserts the
-- row at boot.
CREATE TABLE event_relay_state (
    id        BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    last_seq  BIGINT NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE IF EXISTS event_relay_state;
