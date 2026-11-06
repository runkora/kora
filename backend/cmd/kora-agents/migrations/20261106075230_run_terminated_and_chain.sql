-- +goose Up
-- +goose StatementBegin
-- A terminated run keeps its row and its run_events (docs/adr/2026-11-06-event-spine.md, section 10).
ALTER TABLE runs ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE runs DROP CONSTRAINT runs_state_check;
ALTER TABLE runs ADD CONSTRAINT runs_state_check
  CHECK (state IN ('pending', 'starting', 'active', 'idle', 'completing', 'completed', 'failed', 'budget_exhausted', 'terminated'));
-- The event trigger loop guards read these from every event about the run (section 7).
ALTER TABLE runs ADD COLUMN caused_by UUID;
ALTER TABLE runs ADD COLUMN chain_depth INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN trigger_ids TEXT[] NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN trigger_ids;
ALTER TABLE runs DROP COLUMN chain_depth;
ALTER TABLE runs DROP COLUMN caused_by;
ALTER TABLE runs DROP CONSTRAINT runs_state_check;
ALTER TABLE runs ADD CONSTRAINT runs_state_check
  CHECK (state IN ('pending', 'starting', 'active', 'idle', 'completing', 'completed', 'failed', 'budget_exhausted'));
ALTER TABLE runs DROP COLUMN deleted_at;
-- +goose StatementEnd
