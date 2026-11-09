-- +goose Up
-- Producers in every binary write this table in their own transaction. It is
-- the one cross-binary write path (docs/adr/2026-11-06-event-spine.md).
CREATE TABLE event_outbox (
    id          UUID PRIMARY KEY,
    position    BIGINT GENERATED ALWAYS AS IDENTITY,
    tenant_id   TEXT NOT NULL,
    type        TEXT NOT NULL,
    envelope    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX event_outbox_position_idx ON event_outbox (position);

-- +goose Down
DROP TABLE IF EXISTS event_outbox;
