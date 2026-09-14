-- +goose Up
-- +goose StatementBegin
CREATE TABLE agent_type_drafts (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    created_by TEXT NOT NULL,
    payload JSONB DEFAULT '{}'::jsonb NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT agent_type_drafts_pkey PRIMARY KEY (id)
);

CREATE INDEX idx_agent_type_drafts_owner ON agent_type_drafts (tenant_id, created_by, updated_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS agent_type_drafts;
-- +goose StatementEnd
