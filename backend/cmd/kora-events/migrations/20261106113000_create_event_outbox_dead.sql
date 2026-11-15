-- +goose Up
-- Outbox rows the relay could not record, kept with the reason.
CREATE TABLE event_outbox_dead (
    id          UUID PRIMARY KEY,
    position    BIGINT NOT NULL,
    tenant_id   TEXT NOT NULL,
    type        TEXT NOT NULL,
    envelope    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    reason      TEXT NOT NULL,
    dead_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS event_outbox_dead;
