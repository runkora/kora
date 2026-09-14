-- +goose Up
-- +goose StatementBegin
CREATE TABLE inference_usage (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    principal_type TEXT NOT NULL,
    api_dialect TEXT DEFAULT 'openai' NOT NULL,
    provider TEXT,
    model_id TEXT NOT NULL,
    routing_strategy TEXT,
    prompt_tokens INTEGER DEFAULT 0 NOT NULL,
    completion_tokens INTEGER DEFAULT 0 NOT NULL,
    total_tokens INTEGER DEFAULT 0 NOT NULL,
    estimated_cost_usd NUMERIC(18,10) DEFAULT 0 NOT NULL,
    status TEXT NOT NULL,
    retry_count SMALLINT DEFAULT 0 NOT NULL,
    error_code TEXT,
    latency_ms INTEGER DEFAULT 0 NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    token_id TEXT,
    model_family TEXT,
    cache_read_input_tokens INTEGER DEFAULT 0 NOT NULL,
    cache_creation_input_tokens INTEGER DEFAULT 0 NOT NULL,
    virtual_model_id TEXT,
    failure_details JSONB,
    price_unknown BOOLEAN DEFAULT false NOT NULL,
    effective_input_cost_per_1m NUMERIC(10,6) DEFAULT 0 NOT NULL,
    effective_output_cost_per_1m NUMERIC(10,6) DEFAULT 0 NOT NULL,
    effective_cache_read_cost_per_1m NUMERIC(10,6) DEFAULT 0 NOT NULL,
    effective_cache_write_cost_per_1m NUMERIC(10,6) DEFAULT 0 NOT NULL,
    run_id TEXT,
    principal_roles TEXT[],
    delegator_id TEXT,
    delegator_principal_type TEXT,
    CONSTRAINT inference_usage_pkey PRIMARY KEY (id),
    CONSTRAINT inference_usage_api_dialect_check CHECK (api_dialect IN ('openai', 'anthropic', 'responses')),
    CONSTRAINT inference_usage_principal_type_check CHECK (principal_type IN ('AGENT', 'HUMAN', 'SERVICE', 'SYSTEM', 'PAT')),
    CONSTRAINT inference_usage_provider_check CHECK (provider IS NULL OR provider IN ('bedrock', 'vertexai', 'azure') OR provider ~ '^bedrock:[a-z][a-z0-9_]*$'),
    CONSTRAINT inference_usage_routing_strategy_check CHECK (routing_strategy IN ('lowest_cost', 'failover_chain', 'explicit', 'priority', 'weighted') OR routing_strategy IS NULL),
    CONSTRAINT inference_usage_status_check CHECK (status IN ('success', 'provider_error', 'policy_denied', 'model_not_found', 'budget_exceeded', 'auth_error', 'timeout', 'routing_failed', 'canceled'))
);

CREATE INDEX idx_inference_usage_analytics_personal ON inference_usage (tenant_id, principal_id, created_at DESC);

CREATE INDEX idx_inference_usage_cache_tokens ON inference_usage (tenant_id, cache_read_input_tokens, created_at DESC) WHERE cache_read_input_tokens > 0;

CREATE INDEX idx_inference_usage_created_at ON inference_usage (created_at DESC);

CREATE INDEX idx_inference_usage_delegator ON inference_usage (tenant_id, delegator_id, created_at) WHERE delegator_id IS NOT NULL;

CREATE INDEX idx_inference_usage_model_family ON inference_usage (tenant_id, model_family, created_at DESC) WHERE model_family IS NOT NULL;

CREATE INDEX idx_inference_usage_principal ON inference_usage (tenant_id, principal_id, created_at DESC);

CREATE INDEX idx_inference_usage_principal_roles ON inference_usage USING gin (principal_roles) WHERE principal_roles IS NOT NULL;

CREATE INDEX idx_inference_usage_provider_model ON inference_usage (provider, model_id, created_at DESC);

CREATE INDEX idx_inference_usage_run_id ON inference_usage (run_id) WHERE run_id IS NOT NULL;

CREATE INDEX idx_inference_usage_tenant_id ON inference_usage (tenant_id, created_at DESC);

CREATE INDEX idx_inference_usage_token ON inference_usage (tenant_id, token_id, created_at DESC) WHERE token_id IS NOT NULL;

CREATE INDEX idx_inference_usage_virtual_model ON inference_usage (tenant_id, virtual_model_id, created_at DESC) WHERE virtual_model_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS inference_usage;
-- +goose StatementEnd
