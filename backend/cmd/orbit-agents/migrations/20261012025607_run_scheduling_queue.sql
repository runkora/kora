-- +goose Up
-- +goose StatementBegin
-- Pending runs are a queue that orbit-agents workers claim (docs/adr/2026-10-12-run-scheduling.md).
-- initial_message is sent when the worker creates the session.
ALTER TABLE runs ADD COLUMN initial_message TEXT DEFAULT '' NOT NULL;
-- claimed_at is set while a worker holds the run. Any replica releases a stale claim.
ALTER TABLE runs ADD COLUMN claimed_at TIMESTAMPTZ;
-- not_before is the earliest time a worker may claim the run. A worker never claims a row where it is NULL.
ALTER TABLE runs ADD COLUMN not_before TIMESTAMPTZ;

CREATE INDEX idx_runs_claimable ON runs (created_at) WHERE state = 'pending' AND claimed_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_runs_claimable;
ALTER TABLE runs DROP COLUMN not_before;
ALTER TABLE runs DROP COLUMN claimed_at;
ALTER TABLE runs DROP COLUMN initial_message;
-- +goose StatementEnd
