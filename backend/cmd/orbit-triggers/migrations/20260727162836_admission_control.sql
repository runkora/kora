-- +goose Up
-- +goose StatementBegin

-- Admission control and delivery safety
-- (docs/adr/2026-06-20-extended-trigger-types.md, Decision 5).
--
-- Every fire routes through trigger_pending_deliveries before it dispatches, so
-- an acknowledged fire always starts. The tempting optimisation is a fast path
-- for the uncontended case, but a process dying between recording the delivery
-- and starting the run would then lose that fire silently, leaving a durable
-- record of a start that never happened.

-- ─── Trigger: concurrency ceiling, overflow, and safety settings ──────────────
-- Defaults preserve current behavior: nothing has ever been at a ceiling, so an
-- existing trigger keeps dispatching exactly as it does today.
ALTER TABLE triggers
    ADD COLUMN max_parallel         SMALLINT    NOT NULL DEFAULT 4,
    ADD COLUMN overflow             TEXT        NOT NULL DEFAULT 'wait',
    ADD COLUMN max_queue_depth      SMALLINT    NOT NULL DEFAULT 32,
    ADD COLUMN timeout_seconds      SMALLINT    NOT NULL DEFAULT 30,
    ADD COLUMN dedupe_enabled       BOOLEAN     NOT NULL DEFAULT TRUE,
    ADD COLUMN autopause_enabled    BOOLEAN     NOT NULL DEFAULT TRUE,
    ADD COLUMN consecutive_failures INT         NOT NULL DEFAULT 0,
    ADD COLUMN auto_paused_at       TIMESTAMPTZ;

ALTER TABLE triggers
    ADD CONSTRAINT triggers_overflow_check
    CHECK (overflow IN ('wait', 'replace', 'refuse'));

ALTER TABLE triggers
    ADD CONSTRAINT triggers_max_parallel_check
    CHECK (max_parallel BETWEEN 1 AND 16);

-- ─── The waiting backlog ──────────────────────────────────────────────────────
-- Transient by nature: a row exists only while a fire waits for a slot. It does
-- not replace delivery history, which is already recorded by the time anything
-- lands here.
--
-- claimed_at is nullable rather than a boolean so a stuck claim is visible and
-- can be reaped by age.
CREATE TABLE trigger_pending_deliveries (
    id          TEXT        PRIMARY KEY,
    tenant_id   TEXT        NOT NULL,
    trigger_id  TEXT        NOT NULL,
    delivery_id TEXT        NOT NULL,
    fields      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at  TIMESTAMPTZ
);

-- The claim query: oldest unclaimed first, per trigger. Partial so the index
-- stays small — claimed rows are on their way out.
CREATE INDEX idx_pending_claim
    ON trigger_pending_deliveries (trigger_id, enqueued_at)
    WHERE claimed_at IS NULL;

CREATE INDEX idx_pending_delivery ON trigger_pending_deliveries (delivery_id);

-- ─── Tenant payload retention ─────────────────────────────────────────────────
-- Capped at the 30-day default: a tenant may shorten retention but not extend
-- it, so the platform's exposure to stored third-party payloads has a ceiling
-- no tenant can raise.
CREATE TABLE trigger_retention_settings (
    tenant_id    TEXT     PRIMARY KEY,
    payload_days SMALLINT NOT NULL DEFAULT 30
                 CHECK (payload_days BETWEEN 1 AND 30),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The purge job scans by age; without this it is a sequential scan over every
-- delivery ever recorded.
CREATE INDEX idx_trigger_deliveries_purge
    ON trigger_deliveries (received_at)
    WHERE payload IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_trigger_deliveries_purge;
DROP TABLE IF EXISTS trigger_retention_settings;
DROP TABLE IF EXISTS trigger_pending_deliveries;

ALTER TABLE triggers
    DROP CONSTRAINT IF EXISTS triggers_max_parallel_check,
    DROP CONSTRAINT IF EXISTS triggers_overflow_check;

ALTER TABLE triggers
    DROP COLUMN IF EXISTS auto_paused_at,
    DROP COLUMN IF EXISTS consecutive_failures,
    DROP COLUMN IF EXISTS autopause_enabled,
    DROP COLUMN IF EXISTS dedupe_enabled,
    DROP COLUMN IF EXISTS timeout_seconds,
    DROP COLUMN IF EXISTS max_queue_depth,
    DROP COLUMN IF EXISTS overflow,
    DROP COLUMN IF EXISTS max_parallel;

-- +goose StatementEnd
