-- +goose Up
-- +goose StatementBegin
CREATE TABLE runs (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    agent_type_id TEXT NOT NULL,
    state TEXT DEFAULT 'pending' NOT NULL,
    pod_ordinal INTEGER DEFAULT -1 NOT NULL,
    created_by TEXT DEFAULT '' NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    summary TEXT,
    delegation_jti TEXT,
    trigger_id TEXT,
    trigger_name TEXT,
    delivery_id TEXT,
    CONSTRAINT runs_pkey PRIMARY KEY (id),
    CONSTRAINT runs_state_check CHECK (state IN ('pending', 'starting', 'active', 'idle', 'completing', 'completed', 'failed', 'budget_exhausted')),
    CONSTRAINT runs_agent_type_id_fkey FOREIGN KEY (agent_type_id) REFERENCES agent_types(id)
);

CREATE INDEX idx_runs_active ON runs (agent_type_id, pod_ordinal) WHERE (state IN ('pending', 'starting', 'active', 'idle', 'completing'));

CREATE INDEX idx_runs_agent_type_id ON runs (agent_type_id, created_at DESC);

CREATE INDEX idx_runs_tenant_id ON runs (tenant_id, created_at DESC);

CREATE INDEX idx_runs_trigger_id ON runs (trigger_id, created_at DESC) WHERE (trigger_id IS NOT NULL);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS runs;
-- +goose StatementEnd
