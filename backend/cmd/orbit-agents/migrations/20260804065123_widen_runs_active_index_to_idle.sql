-- Widen idx_runs_active to include 'idle'.
--
-- An idle run has finished its turn but its session process is still resident
-- on the pod, so it occupies a slot for MaxConcurrentRunsPerPod. The per-pod
-- occupancy count now includes it (agents.RunState.OccupiesPod), and this
-- partial index has to carry the same predicate or the planner can no longer
-- use it for that query.

-- +goose Up
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_runs_active;

CREATE INDEX IF NOT EXISTS idx_runs_active
    ON runs (agent_type_id, pod_ordinal)
    WHERE state IN ('pending', 'starting', 'active', 'idle', 'completing');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_runs_active;

CREATE INDEX IF NOT EXISTS idx_runs_active
    ON runs (agent_type_id, pod_ordinal)
    WHERE state IN ('pending', 'starting', 'active', 'completing');
-- +goose StatementEnd
