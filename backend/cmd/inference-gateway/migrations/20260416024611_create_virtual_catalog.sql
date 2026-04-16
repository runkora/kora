-- +goose Up
-- +goose StatementBegin

-- ─── virtual_models ───────────────────────────────────────────────────────────
-- Platform-curated model definitions. Each virtual model is the sole consumer-
-- facing model identity. Provider details are encapsulated in virtual_model_routes
-- and never surfaced externally.
--
-- The (family, name) pair maps directly to the policy scope:
--   inference:{family}:{name}:use
-- e.g. family="anthropic", name="claude-sonnet-4-6"
--   → inference:anthropic:claude-sonnet-4-6:use
CREATE TABLE IF NOT EXISTS virtual_models (
    id           TEXT        PRIMARY KEY,
    family       TEXT        NOT NULL,
    name         TEXT        NOT NULL,
    display_name TEXT        NOT NULL,
    description  TEXT        NOT NULL DEFAULT '',
    strategy     TEXT        NOT NULL
        CHECK (strategy IN ('priority', 'weighted', 'lowest_cost')),
    enabled      BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (family, name)
);

-- ─── virtual_model_routes ─────────────────────────────────────────────────────
-- Ordered provider targets for each virtual model. The routing engine selects
-- among enabled routes according to the virtual model's strategy.
CREATE TABLE IF NOT EXISTS virtual_model_routes (
    id                  TEXT           PRIMARY KEY,
    virtual_model_id    TEXT           NOT NULL REFERENCES virtual_models(id) ON DELETE CASCADE,
    provider            TEXT           NOT NULL
        CHECK (provider IN ('bedrock', 'vertexai', 'azure')),
    model_id            TEXT           NOT NULL, -- canonical provider model ID, no provider/ prefix
    priority            INT            NOT NULL DEFAULT 1,
    weight              INT            NOT NULL DEFAULT 0,       -- pct 0–100; weighted strategy only
    input_cost_per_1m   NUMERIC(10, 6) NOT NULL DEFAULT 0,      -- USD per 1M input tokens
    output_cost_per_1m  NUMERIC(10, 6) NOT NULL DEFAULT 0,      -- USD per 1M output tokens
    enabled             BOOLEAN        NOT NULL DEFAULT TRUE,
    UNIQUE (virtual_model_id, provider, model_id)
);

CREATE INDEX IF NOT EXISTS idx_virtual_model_routes_vm_priority
    ON virtual_model_routes (virtual_model_id, priority ASC);

-- ─── provider_model_overrides ─────────────────────────────────────────────────
-- Stores platform admin enable/disable decisions for provider-discovered models.
-- Absence of a row means the provider model is enabled (default-allow).
-- Disabled provider models are excluded from route target resolution.
CREATE TABLE IF NOT EXISTS provider_model_overrides (
    provider    TEXT        NOT NULL
        CHECK (provider IN ('bedrock', 'vertexai', 'azure')),
    model_id    TEXT        NOT NULL,
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, model_id)
);

-- ─── Extend routing_strategy CHECK ───────────────────────────────────────────
-- Add 'priority' and 'weighted' to the inference_usage routing_strategy
-- constraint to cover virtual model routing strategies.
ALTER TABLE inference_usage
    DROP CONSTRAINT IF EXISTS inference_usage_routing_strategy_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_routing_strategy_check
    CHECK (
        routing_strategy IN ('lowest_cost', 'failover_chain', 'explicit', 'priority', 'weighted')
        OR routing_strategy IS NULL
    );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE inference_usage
    DROP CONSTRAINT IF EXISTS inference_usage_routing_strategy_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_routing_strategy_check
    CHECK (
        routing_strategy IN ('lowest_cost', 'failover_chain', 'explicit')
        OR routing_strategy IS NULL
    );

DROP TABLE IF EXISTS provider_model_overrides;
DROP TABLE IF EXISTS virtual_model_routes;
DROP TABLE IF EXISTS virtual_models;

-- +goose StatementEnd
