-- +goose Up
-- +goose StatementBegin
-- A run records the name of its bound pool pod, which replaces pod_ordinal
-- (docs/adr/2026-11-22-agent-cold-tier.md). pod_ordinal stays for release N-1 until a contract step drops it.
ALTER TABLE runs ADD COLUMN pod_name TEXT;

-- Serves the per-pod run counts of placement and the scaler. The predicate lists the states RunState.OccupiesPod holds.
CREATE INDEX idx_runs_active_pod_name ON runs (agent_type_id, pod_name) WHERE state IN ('pending', 'starting', 'active', 'idle', 'completing');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_runs_active_pod_name;
ALTER TABLE runs DROP COLUMN IF EXISTS pod_name;
-- +goose StatementEnd
