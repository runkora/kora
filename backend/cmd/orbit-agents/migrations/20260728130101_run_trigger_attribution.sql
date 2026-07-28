-- +goose Up
-- +goose StatementBegin

-- The run half of the trigger backlink (US-004 AC3/AC4).
--
-- The forward direction already existed: a trigger delivery records the run it
-- started. The reverse did not, so a run opened from the runs list could not say
-- it was started by a trigger at all — its created_by is the trigger's SERVICE
-- principal, which is an opaque id to a person reading it.
--
-- Three explicit nullable columns rather than a JSONB bag: runs has no metadata
-- mechanism to reuse, and summary and delegation_jti set the precedent that a
-- new fact about a run is a typed column. NULL means "not started by a trigger",
-- which is every human-started run and every run that predates this migration.
--
-- trigger_name is denormalized deliberately. It is the name AT START TIME, and
-- it is stored precisely so a renamed or deleted trigger still reads as
-- something a person recognizes. It is not a foreign key for the same reason:
-- runs are audit history and must outlive the trigger that started them.
ALTER TABLE runs
    ADD COLUMN trigger_id   TEXT,
    ADD COLUMN trigger_name TEXT,
    ADD COLUMN delivery_id  TEXT;

-- "Show me everything this trigger has started" is the query this link exists to
-- make possible; partial so the index covers only trigger-started runs.
CREATE INDEX idx_runs_trigger_id
    ON runs (trigger_id, created_at DESC)
    WHERE trigger_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_runs_trigger_id;

ALTER TABLE runs
    DROP COLUMN IF EXISTS delivery_id,
    DROP COLUMN IF EXISTS trigger_name,
    DROP COLUMN IF EXISTS trigger_id;

-- +goose StatementEnd
