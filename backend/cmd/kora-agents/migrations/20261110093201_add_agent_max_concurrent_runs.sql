-- +goose Up
-- +goose StatementBegin
-- The total run cap across an agent's pods replaces the per-pod cap (docs/adr/2026-11-09-demand-driven-agent-scaling.md).
-- The old column stays for N-1 binaries until a later contract step drops it.
ALTER TABLE agent_types ADD COLUMN max_concurrent_runs INTEGER NOT NULL DEFAULT 20;
UPDATE agent_types SET max_concurrent_runs = max_concurrent_runs_per_pod;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN max_concurrent_runs;
-- +goose StatementEnd
