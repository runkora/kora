-- +goose Up
-- +goose StatementBegin

-- Delivery outcome model (docs/adr/2026-06-20-extended-trigger-types.md, Decision 3).
--
-- Outcomes live at two levels, and conflating them is the main way this design
-- goes wrong:
--
--   trigger_deliveries.status     what happened to the arrival itself
--   trigger_delivery_runs.status  what happened for each trigger it matched
--
-- Every change here is additive. No column is dropped or renamed and no existing
-- row changes meaning: the values in use today remain valid members of the
-- widened vocabularies.

-- ─── Trigger target (Decision 8) ──────────────────────────────────────────────
-- Triggers are the platform's front door for starting work. What gets started is
-- a separate concern: 'agent' is the only legal value today, and 'workflow'
-- becomes legal when that pillar lands, without changing what a trigger is or
-- how it is authorised.
ALTER TABLE triggers
    ADD COLUMN target_kind TEXT NOT NULL DEFAULT 'agent';

ALTER TABLE triggers
    ADD CONSTRAINT triggers_target_kind_check
    CHECK (target_kind IN ('agent'));

-- ─── Delivery: full payload, reason, redelivery lineage ───────────────────────
-- payload supersedes payload_excerpt, which is kept until the console reads the
-- full body everywhere (dropped in a later migration). payload_purged records
-- that retention has run, so an empty payload is distinguishable from one that
-- was never captured.
ALTER TABLE trigger_deliveries
    ADD COLUMN payload        BYTEA,
    ADD COLUMN payload_bytes  INT,
    ADD COLUMN payload_purged BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN reason         TEXT,
    ADD COLUMN redelivery_of  TEXT;

-- ─── Delivery status vocabulary ───────────────────────────────────────────────
-- Existing values: verified, dispatched, skipped, failed.
-- Added: duplicate (already handled; references the original) and missed (a
-- scheduled fire that was due but not run).
--
-- Deliberately absent: any value for failed authentication. A webhook with a bad
-- signature is an unauthenticated request, so recording it here would let anyone
-- on the internet create rows. It updates a counter on the source instead
-- (Decision 4), which means every row in this table is an arrival that
-- authenticated successfully.
ALTER TABLE trigger_deliveries
    ADD CONSTRAINT trigger_deliveries_status_check
    CHECK (status IN ('verified', 'dispatched', 'skipped', 'failed',
                      'duplicate', 'missed'));

-- ─── Per-trigger outcome vocabulary ───────────────────────────────────────────
-- Existing values: dispatched, failed.
-- Added: filtered (listening, but this trigger's match_condition excluded the
-- event), denied (policy refused), superseded (replaced by a newer fire under
-- 'replace' overflow), dropped (refused at the concurrency ceiling or evicted
-- from the backlog), paused (the trigger was auto-paused when this fire arrived).
ALTER TABLE trigger_delivery_runs
    ADD CONSTRAINT trigger_delivery_runs_status_check
    CHECK (status IN ('dispatched', 'failed',
                      'filtered', 'denied', 'superseded', 'dropped', 'paused'));

-- ─── Source rejection counters (Decision 4) ───────────────────────────────────
-- Failed authentication never creates a delivery row. It updates the source the
-- tenant already owns, so an unauthenticated request cannot grow any table no
-- matter what it sends. Only the most recent reason is kept; the exact count of
-- rejected requests belongs in metrics, not in history.
ALTER TABLE webhook_sources
    ADD COLUMN rejection_count       BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN last_rejection_at     TIMESTAMPTZ,
    ADD COLUMN last_rejection_reason TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE webhook_sources
    DROP COLUMN IF EXISTS last_rejection_reason,
    DROP COLUMN IF EXISTS last_rejection_at,
    DROP COLUMN IF EXISTS rejection_count;

ALTER TABLE trigger_delivery_runs
    DROP CONSTRAINT IF EXISTS trigger_delivery_runs_status_check;

ALTER TABLE trigger_deliveries
    DROP CONSTRAINT IF EXISTS trigger_deliveries_status_check;

ALTER TABLE trigger_deliveries
    DROP COLUMN IF EXISTS redelivery_of,
    DROP COLUMN IF EXISTS reason,
    DROP COLUMN IF EXISTS payload_purged,
    DROP COLUMN IF EXISTS payload_bytes,
    DROP COLUMN IF EXISTS payload;

ALTER TABLE triggers
    DROP CONSTRAINT IF EXISTS triggers_target_kind_check;

ALTER TABLE triggers
    DROP COLUMN IF EXISTS target_kind;

-- +goose StatementEnd
