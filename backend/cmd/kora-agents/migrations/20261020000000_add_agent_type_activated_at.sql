-- +goose Up
-- +goose StatementBegin
-- When the agent type last became active. The run start budget is measured from it, so provisioning time
-- is excluded and an agent edit does not restart the budget. NULL while the type has never been active.
ALTER TABLE agent_types ADD COLUMN activated_at TIMESTAMPTZ;
UPDATE agent_types SET activated_at = created_at WHERE state = 'active';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN activated_at;
-- +goose StatementEnd
