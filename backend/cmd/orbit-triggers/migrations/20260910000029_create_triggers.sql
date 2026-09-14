-- +goose Up
-- +goose StatementBegin
CREATE TABLE triggers (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    source_id TEXT,
    name TEXT NOT NULL,
    agent_type_id TEXT NOT NULL,
    agent_slug TEXT NOT NULL,
    event_type TEXT NOT NULL,
    match_condition JSONB DEFAULT '{}'::jsonb NOT NULL,
    prompt_template TEXT NOT NULL,
    grant_id TEXT,
    enabled BOOLEAN DEFAULT true NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    kind TEXT DEFAULT 'webhook' NOT NULL,
    kind_config JSONB DEFAULT '{}'::jsonb NOT NULL,
    principal_id TEXT,
    grant_id_svc TEXT,
    target_kind TEXT DEFAULT 'agent' NOT NULL,
    max_parallel SMALLINT DEFAULT 4 NOT NULL,
    overflow TEXT DEFAULT 'wait' NOT NULL,
    max_queue_depth SMALLINT DEFAULT 32 NOT NULL,
    timeout_seconds SMALLINT DEFAULT 30 NOT NULL,
    dedupe_enabled BOOLEAN DEFAULT true NOT NULL,
    autopause_enabled BOOLEAN DEFAULT true NOT NULL,
    consecutive_failures INTEGER DEFAULT 0 NOT NULL,
    auto_paused_at TIMESTAMPTZ,
    CONSTRAINT triggers_pkey PRIMARY KEY (id),
    CONSTRAINT triggers_max_parallel_check CHECK ((max_parallel >= 1) AND (max_parallel <= 16)),
    CONSTRAINT triggers_overflow_check CHECK (overflow IN ('wait', 'replace', 'refuse')),
    CONSTRAINT triggers_target_kind_check CHECK (target_kind = 'agent'),
    CONSTRAINT triggers_source_id_fkey FOREIGN KEY (source_id) REFERENCES webhook_sources(id) ON DELETE CASCADE
);

CREATE INDEX idx_triggers_kind ON triggers (kind) WHERE (enabled = true);

CREATE INDEX idx_triggers_source_id ON triggers (source_id);

CREATE INDEX idx_triggers_tenant_agent ON triggers (tenant_id, agent_type_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS triggers;
-- +goose StatementEnd
