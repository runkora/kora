-- +goose Up
-- +goose StatementBegin

-- ─── inference_usage ─────────────────────────────────────────────────────────
-- One row per completed (or failed) inference request.
-- Attribution is per-principal so cost and usage can be aggregated at any
-- grain: tenant, principal_type, individual principal, or model/provider.

CREATE TABLE IF NOT EXISTS inference_usage (
    id                  TEXT        PRIMARY KEY,                      -- UUID v4
    tenant_id           TEXT        NOT NULL,
    principal_id        TEXT        NOT NULL,
    principal_type      TEXT        NOT NULL
        CHECK (principal_type IN ('AGENT', 'HUMAN', 'SERVICE', 'SYSTEM')),
    api_dialect         TEXT        NOT NULL DEFAULT 'openai'
        CHECK (api_dialect IN ('openai', 'anthropic')),
    provider            TEXT                                          -- NULL when routing failed before selection
        CHECK (provider IN ('bedrock', 'vertexai', 'azure') OR provider IS NULL),
    model_id            TEXT        NOT NULL,
    routing_strategy    TEXT                                          -- NULL when routing failed
        CHECK (routing_strategy IN ('lowest_cost', 'failover_chain', 'explicit') OR routing_strategy IS NULL),
    prompt_tokens       INTEGER     NOT NULL DEFAULT 0,
    completion_tokens   INTEGER     NOT NULL DEFAULT 0,
    total_tokens        INTEGER     NOT NULL DEFAULT 0,
    estimated_cost_usd  NUMERIC(18, 10) NOT NULL DEFAULT 0,
    status              TEXT        NOT NULL
        CHECK (status IN ('success', 'provider_error', 'policy_denied', 'auth_error', 'timeout', 'routing_failed')),
    retry_count         SMALLINT    NOT NULL DEFAULT 0,
    error_code          TEXT,                                         -- provider error code or sentinel name
    latency_ms          INTEGER     NOT NULL DEFAULT 0,               -- wall-clock from first byte to last
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_inference_usage_tenant_id
    ON inference_usage (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_inference_usage_principal_id
    ON inference_usage (principal_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_inference_usage_provider_model
    ON inference_usage (provider, model_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_inference_usage_created_at
    ON inference_usage (created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS inference_usage;

-- +goose StatementEnd
