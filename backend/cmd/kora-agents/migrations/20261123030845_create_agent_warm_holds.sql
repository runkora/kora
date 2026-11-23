-- +goose Up
-- +goose StatementBegin
-- A scheduled pre-wake holds an agent warm until shortly after the schedule fires
-- (docs/adr/2026-11-22-agent-cold-tier.md, S2). The scaler keeps the agent's floor
-- of pods at full size while the hold lasts.
CREATE TABLE agent_warm_holds (
    agent_type_id TEXT PRIMARY KEY,
    until TIMESTAMPTZ NOT NULL,
    CONSTRAINT agent_warm_holds_agent_type_id_fkey FOREIGN KEY (agent_type_id) REFERENCES agent_types(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS agent_warm_holds;
-- +goose StatementEnd
