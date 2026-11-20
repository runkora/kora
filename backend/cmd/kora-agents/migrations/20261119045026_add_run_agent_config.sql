-- +goose Up
-- +goose StatementBegin
-- A digest of the agent's effective config when the run started. Graders group results by it. See docs/adr/2026-11-19-graders.md.
ALTER TABLE runs ADD COLUMN agent_config TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN agent_config;
-- +goose StatementEnd
