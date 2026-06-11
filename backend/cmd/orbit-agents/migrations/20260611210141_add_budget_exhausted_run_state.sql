-- +goose Up
-- +goose StatementBegin
ALTER TABLE runs DROP CONSTRAINT runs_state_check;
ALTER TABLE runs ADD CONSTRAINT runs_state_check
    CHECK (state IN ('pending', 'starting', 'active', 'idle', 'completing', 'completed', 'failed', 'budget_exhausted'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE runs SET state = 'failed' WHERE state = 'budget_exhausted';
ALTER TABLE runs DROP CONSTRAINT runs_state_check;
ALTER TABLE runs ADD CONSTRAINT runs_state_check
    CHECK (state IN ('pending', 'starting', 'active', 'idle', 'completing', 'completed', 'failed'));
-- +goose StatementEnd
