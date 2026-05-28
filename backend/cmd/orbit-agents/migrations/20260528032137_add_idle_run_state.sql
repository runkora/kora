-- +goose Up
-- +goose StatementBegin
ALTER TABLE runs DROP CONSTRAINT runs_state_check;
ALTER TABLE runs ADD CONSTRAINT runs_state_check
    CHECK (state IN ('pending', 'starting', 'active', 'idle', 'completing', 'completed', 'failed'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP CONSTRAINT runs_state_check;
ALTER TABLE runs ADD CONSTRAINT runs_state_check
    CHECK (state IN ('pending', 'starting', 'active', 'completing', 'completed', 'failed'));
-- +goose StatementEnd
