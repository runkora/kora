-- +goose Up
-- +goose StatementBegin
-- The scaler reads an agent's latest completed run to decide when its pods turn warm
-- (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 3).
CREATE INDEX idx_runs_agent_completed ON runs (agent_type_id, completed_at DESC) WHERE completed_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_runs_agent_completed;
-- +goose StatementEnd
