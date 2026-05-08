-- +goose Up
-- +goose StatementBegin

-- ─── agent_types ─────────────────────────────────────────────────────────────
-- One row per managed agent type per tenant. Each active agent type has a
-- corresponding StatefulSet in orbit-tenant-{tenant_id}.

CREATE TABLE IF NOT EXISTS agent_types (
    id                          TEXT        PRIMARY KEY,
    tenant_id                   TEXT        NOT NULL,
    name                        TEXT        NOT NULL,
    slug                        TEXT        NOT NULL,
    tool_scopes                 TEXT[]      NOT NULL DEFAULT '{}',
    max_concurrent_runs_per_pod INTEGER     NOT NULL DEFAULT 10,
    state                       TEXT        NOT NULL DEFAULT 'provisioning'
        CHECK (state IN ('provisioning', 'active', 'deleting')),
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_agent_types_tenant_id
    ON agent_types (tenant_id, created_at DESC);

-- ─── runs ─────────────────────────────────────────────────────────────────────
-- One row per unit of work. Outlives browser connections; persists until
-- completed, failed, or explicitly deleted.

CREATE TABLE IF NOT EXISTS runs (
    id              TEXT        PRIMARY KEY,
    tenant_id       TEXT        NOT NULL,
    agent_type_id   TEXT        NOT NULL REFERENCES agent_types(id),
    state           TEXT        NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'starting', 'active', 'completing', 'completed', 'failed')),
    pod_ordinal     INTEGER     NOT NULL DEFAULT -1,
    created_by      TEXT        NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_runs_tenant_id
    ON runs (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_runs_agent_type_id
    ON runs (agent_type_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_runs_active
    ON runs (agent_type_id, pod_ordinal)
    WHERE state IN ('pending', 'starting', 'active', 'completing');

-- ─── sessions ─────────────────────────────────────────────────────────────────
-- Transient browser connections to a run. Disconnect does not affect the run.

CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT        PRIMARY KEY,
    run_id          TEXT        NOT NULL REFERENCES runs(id),
    principal_id    TEXT        NOT NULL,
    connected_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    disconnected_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_sessions_run_id
    ON sessions (run_id);

-- ─── run_events ───────────────────────────────────────────────────────────────
-- Append-only event log. Source of truth for run history and SSE replay.

CREATE TABLE IF NOT EXISTS run_events (
    sequence_no BIGSERIAL   PRIMARY KEY,
    run_id      TEXT        NOT NULL REFERENCES runs(id),
    event_type  TEXT        NOT NULL,
    payload     JSONB       NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_run_events_run_id
    ON run_events (run_id, sequence_no);

-- ─── run_messages ─────────────────────────────────────────────────────────────
-- User messages sent to a run. Separate from events (events are agent output).

CREATE TABLE IF NOT EXISTS run_messages (
    id          TEXT        PRIMARY KEY,
    run_id      TEXT        NOT NULL REFERENCES runs(id),
    sequence_no BIGSERIAL   NOT NULL,
    content     TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_run_messages_run_id
    ON run_messages (run_id, sequence_no);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS run_messages;
DROP TABLE IF EXISTS run_events;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS runs;
DROP TABLE IF EXISTS agent_types;

-- +goose StatementEnd
