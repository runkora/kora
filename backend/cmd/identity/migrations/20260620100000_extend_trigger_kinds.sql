-- +goose Up
-- +goose StatementBegin

-- ─── Extend triggers with kind discriminant ───────────────────────────────────
-- Adds support for 'scheduled' and 'manual' trigger kinds alongside the
-- existing 'webhook' kind. source_id becomes nullable (scheduled/manual triggers
-- have no webhook source). kind_config carries kind-specific fields (cron
-- expression + timezone for scheduled; empty for others). principal_id and
-- grant_id on the trigger itself hold the SERVICE identity for scheduled/manual
-- triggers (webhook triggers inherit their principal from webhook_sources).
-- See docs/adr/2026-06-20-extended-trigger-types.md.

ALTER TABLE triggers
    ADD COLUMN kind         TEXT    NOT NULL DEFAULT 'webhook',
    ADD COLUMN kind_config  JSONB   NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN principal_id TEXT,
    ADD COLUMN grant_id_svc TEXT;   -- trigger-level SERVICE principal grant (scheduled/manual)

-- Make source_id nullable: scheduled and manual triggers have no webhook source.
ALTER TABLE triggers
    ALTER COLUMN source_id DROP NOT NULL;

-- Index for the scheduler to efficiently query enabled scheduled triggers.
CREATE INDEX idx_triggers_kind ON triggers (kind) WHERE enabled = true;

-- ─── Extend trigger_deliveries for new kinds ─────────────────────────────────
-- kind is denormalised from the trigger at insert time so delivery history
-- survives trigger deletion. fired_by records the calling principal ID for
-- manual trigger fires. scheduled_at records the cron tick timestamp for
-- scheduled trigger fires. A partial unique index on (trigger_id,
-- scheduled_at) prevents double-fire when the scheduler restarts within the
-- same minute window. source_id becomes nullable: scheduled and manual
-- deliveries have no webhook source, only a trigger_id.

ALTER TABLE trigger_deliveries
    ADD COLUMN kind         TEXT NOT NULL DEFAULT 'webhook',
    ADD COLUMN fired_by     TEXT,
    ADD COLUMN scheduled_at TIMESTAMPTZ;

ALTER TABLE trigger_deliveries
    ALTER COLUMN source_id DROP NOT NULL;

CREATE UNIQUE INDEX idx_trigger_deliveries_scheduled_dedup
    ON trigger_deliveries (trigger_id, scheduled_at)
    WHERE scheduled_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_trigger_deliveries_scheduled_dedup;

ALTER TABLE trigger_deliveries
    ALTER COLUMN source_id SET NOT NULL;

ALTER TABLE trigger_deliveries
    DROP COLUMN IF EXISTS scheduled_at,
    DROP COLUMN IF EXISTS fired_by,
    DROP COLUMN IF EXISTS kind;

DROP INDEX IF EXISTS idx_triggers_kind;

ALTER TABLE triggers
    ALTER COLUMN source_id SET NOT NULL;

ALTER TABLE triggers
    DROP COLUMN IF EXISTS grant_id_svc,
    DROP COLUMN IF EXISTS principal_id,
    DROP COLUMN IF EXISTS kind_config,
    DROP COLUMN IF EXISTS kind;

-- +goose StatementEnd
