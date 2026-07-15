-- +goose Up
-- +goose StatementBegin

-- ─── dev_environment_audit_events ──────────────────────────────────────────────
-- Best-effort audit projection for the devenvironments feature: every
-- API-initiated action (created/extended/woken/kubeconfig_issued/destroyed/
-- force_destroyed) writes synchronously in the same service call that
-- performs it; every operator-driven transition the API layer otherwise has
-- no way to observe (provision_ready/provision_failed/auto_slept/expired)
-- is derived by a CRD-watch ingester. When the platform event spine ships,
-- emission moves onto it — this table remains as the queryable projection.
CREATE TABLE dev_environment_audit_events (
    id             BIGSERIAL PRIMARY KEY,
    environment_id TEXT NOT NULL,
    action         TEXT NOT NULL
        CHECK (action IN (
            'created', 'provision_ready', 'provision_failed', 'extended',
            'woken', 'kubeconfig_issued', 'auto_slept', 'expired',
            'destroyed', 'force_destroyed'
        )),
    actor          TEXT NOT NULL,          -- verified platform token subject, or "system:operator"
    actor_type     TEXT NOT NULL CHECK (actor_type IN ('user', 'system')),
    occurred_at    TIMESTAMPTZ NOT NULL,
    details        JSONB,
    -- The natural idempotency key: a replayed derivation (ingester restart,
    -- redelivered watch event) or a retried API call must never duplicate
    -- the same (environment, action) pair observed at the same instant.
    UNIQUE (environment_id, action, occurred_at)
);

CREATE INDEX idx_dev_environment_audit_events_environment_id
    ON dev_environment_audit_events (environment_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS dev_environment_audit_events;

-- +goose StatementEnd
