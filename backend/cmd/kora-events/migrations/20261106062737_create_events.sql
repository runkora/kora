-- +goose Up
-- The event log. Only the relay writes it, and seq is gapless in relay order.
-- The indexed columns copy envelope attributes for filtering.
CREATE TABLE events (
    seq             BIGINT PRIMARY KEY,
    id              UUID NOT NULL UNIQUE,
    tenant_id       TEXT NOT NULL,
    type            TEXT NOT NULL,
    category        TEXT NOT NULL,
    subject         TEXT NOT NULL,
    principal       TEXT NOT NULL,
    principal_type  TEXT NOT NULL,
    run_id          UUID,
    agent           TEXT,
    caused_by       UUID,
    chain_depth     INT NOT NULL DEFAULT 0,
    occurred_at     TIMESTAMPTZ NOT NULL,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    envelope        JSONB NOT NULL
);
CREATE INDEX events_tenant_seq_idx ON events (tenant_id, seq DESC);
CREATE INDEX events_tenant_category_seq_idx ON events (tenant_id, category, seq DESC);
CREATE INDEX events_tenant_type_seq_idx ON events (tenant_id, type, seq DESC);
CREATE INDEX events_tenant_principal_seq_idx ON events (tenant_id, principal, seq DESC);
CREATE INDEX events_tenant_agent_seq_idx ON events (tenant_id, agent, seq DESC) WHERE agent IS NOT NULL;
CREATE INDEX events_run_idx ON events (run_id) WHERE run_id IS NOT NULL;
CREATE INDEX events_recorded_at_idx ON events (recorded_at);

-- +goose Down
DROP TABLE IF EXISTS events;
