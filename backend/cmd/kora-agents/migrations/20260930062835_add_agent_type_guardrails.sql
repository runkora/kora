-- +goose Up
-- +goose StatementBegin
-- An agent type's loop-guard thresholds (docs/adr/2026-09-29-agent-run-guardrails.md,
-- Amendment 1). NULL, or an absent key, takes the platform default.
ALTER TABLE agent_types ADD COLUMN guardrails JSONB;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN guardrails;
-- +goose StatementEnd
