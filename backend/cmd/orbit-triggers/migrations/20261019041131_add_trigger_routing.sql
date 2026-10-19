-- +goose Up
-- A routed trigger names its candidate agents in trigger_route_candidates and has no agent of its own.
-- +goose StatementBegin
ALTER TABLE triggers DROP CONSTRAINT triggers_target_kind_check;
ALTER TABLE triggers ALTER COLUMN agent_type_id DROP NOT NULL;
ALTER TABLE triggers ALTER COLUMN agent_slug DROP NOT NULL;
ALTER TABLE triggers ADD CONSTRAINT triggers_target_kind_check CHECK (
    (target_kind = 'agent' AND agent_type_id IS NOT NULL)
    OR (target_kind = 'routed' AND agent_type_id IS NULL)
);
-- +goose StatementEnd

-- The routing settings of a routed trigger.
-- +goose StatementBegin
CREATE TABLE trigger_routes (
    trigger_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    model TEXT NOT NULL,
    confidence TEXT DEFAULT 'balanced' NOT NULL,
    fallback_agent_type_id TEXT,
    join_runs BOOLEAN DEFAULT true NOT NULL,
    join_key TEXT,
    CONSTRAINT trigger_routes_pkey PRIMARY KEY (trigger_id),
    CONSTRAINT trigger_routes_trigger_id_fkey FOREIGN KEY (trigger_id) REFERENCES triggers(id) ON DELETE CASCADE,
    CONSTRAINT trigger_routes_confidence_check CHECK (confidence IN ('strict', 'balanced', 'loose')),
    CONSTRAINT trigger_routes_join_key_length_check CHECK (join_key IS NULL OR octet_length(join_key) <= 256)
);
-- +goose StatementEnd

-- The agents a routed trigger may start or join. The slug keys the run grant.
-- +goose StatementBegin
CREATE TABLE trigger_route_candidates (
    trigger_id TEXT NOT NULL,
    agent_type_id TEXT NOT NULL,
    agent_slug TEXT NOT NULL,
    CONSTRAINT trigger_route_candidates_pkey PRIMARY KEY (trigger_id, agent_type_id),
    CONSTRAINT trigger_route_candidates_trigger_id_fkey FOREIGN KEY (trigger_id) REFERENCES triggers(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- One routing decision per routed delivery and trigger, deleted with its delivery.
-- +goose StatementBegin
CREATE TABLE trigger_routing_decisions (
    delivery_id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    question_version TEXT NOT NULL,
    model TEXT,
    outcome TEXT NOT NULL,
    join_key TEXT,
    key_matched BOOLEAN DEFAULT false NOT NULL,
    agent_type_id TEXT,
    run_id TEXT,
    threshold NUMERIC(4, 3),
    probability NUMERIC(4, 3),
    confidence NUMERIC(4, 3),
    scores JSONB DEFAULT '{}'::jsonb NOT NULL,
    fallback_reason TEXT,
    error TEXT,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT trigger_routing_decisions_pkey PRIMARY KEY (delivery_id, trigger_id),
    CONSTRAINT trigger_routing_decisions_delivery_id_fkey FOREIGN KEY (delivery_id) REFERENCES trigger_deliveries(id) ON DELETE CASCADE,
    CONSTRAINT trigger_routing_decisions_outcome_check CHECK (outcome IN ('new_run', 'joined_run', 'fallback_run', 'no_run')),
    CONSTRAINT trigger_routing_decisions_join_key_length_check CHECK (join_key IS NULL OR octet_length(join_key) <= 256)
);
-- +goose StatementEnd

-- The key-first lookup: the newest decision of a trigger with a key and a run.
-- +goose StatementBegin
CREATE INDEX trigger_routing_decisions_join_key_idx
    ON trigger_routing_decisions (trigger_id, join_key, created_at DESC)
    WHERE join_key IS NOT NULL AND run_id IS NOT NULL;
-- +goose StatementEnd

-- joined records a delivery that joined a run, unrouted one that started nothing under the no-run fallback.
-- +goose StatementBegin
ALTER TABLE trigger_delivery_runs DROP CONSTRAINT trigger_delivery_runs_status_check;
ALTER TABLE trigger_delivery_runs ADD CONSTRAINT trigger_delivery_runs_status_check
    CHECK (status IN ('dispatched', 'joined', 'unrouted', 'failed', 'filtered', 'denied', 'superseded', 'dropped', 'paused'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE trigger_delivery_runs DROP CONSTRAINT trigger_delivery_runs_status_check;
ALTER TABLE trigger_delivery_runs ADD CONSTRAINT trigger_delivery_runs_status_check
    CHECK (status IN ('dispatched', 'failed', 'filtered', 'denied', 'superseded', 'dropped', 'paused'));
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS trigger_routing_decisions;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS trigger_route_candidates;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS trigger_routes;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE triggers DROP CONSTRAINT triggers_target_kind_check;
ALTER TABLE triggers ALTER COLUMN agent_slug SET NOT NULL;
ALTER TABLE triggers ALTER COLUMN agent_type_id SET NOT NULL;
ALTER TABLE triggers ADD CONSTRAINT triggers_target_kind_check CHECK (target_kind = 'agent');
-- +goose StatementEnd
