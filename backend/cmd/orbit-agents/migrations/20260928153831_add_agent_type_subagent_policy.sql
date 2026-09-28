-- +goose Up
-- +goose StatementBegin
-- Subagent policy per agent type (docs/adr/2026-09-29-agent-run-guardrails.md).
-- NULL max_concurrent_subagents leaves the Claude Code default in force.
ALTER TABLE agent_types
    ADD COLUMN max_concurrent_subagents INTEGER CHECK (max_concurrent_subagents IS NULL OR max_concurrent_subagents >= 1),
    ADD COLUMN nested_subagents BOOLEAN NOT NULL DEFAULT TRUE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types
    DROP COLUMN max_concurrent_subagents,
    DROP COLUMN nested_subagents;
-- +goose StatementEnd
