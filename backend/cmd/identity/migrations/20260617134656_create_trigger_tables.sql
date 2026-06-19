-- +goose Up
-- +goose StatementBegin

-- ─── webhook_sources ──────────────────────────────────────────────────────────
-- A webhook source is a registered inbound origin of webhook deliveries (e.g. a
-- GitHub repository). It is the trust anchor for the Triggers feature: it owns the
-- HMAC signing secret used to verify deliveries and a mapped SERVICE principal
-- under which the runs its triggers start execute.
--
-- The signing secret is stored AES-GCM encrypted (never plaintext) and is shown
-- to the operator exactly once at creation. principal_id is a stable SERVICE
-- principal identifier; grant_id references the policy grant that gives that
-- principal the generic agents:runs scopes (per-agent scopes are granted per
-- trigger — see the triggers table). See
-- docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md.
CREATE TABLE webhook_sources (
    id                  TEXT        PRIMARY KEY,
    tenant_id           TEXT        NOT NULL,
    name                TEXT        NOT NULL,
    provider            TEXT        NOT NULL,                 -- source adapter, e.g. "github"
    principal_id        TEXT        NOT NULL,                 -- mapped SERVICE principal id
    grant_id            TEXT,                                 -- policy grant for generic agents:runs scopes
    secret_ciphertext   TEXT        NOT NULL,                 -- AES-GCM encrypted HMAC signing secret (base64)
    config              JSONB       NOT NULL DEFAULT '{}'::jsonb, -- provider-specific config
    enabled             BOOLEAN     NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX idx_webhook_sources_tenant_id ON webhook_sources (tenant_id);

-- ─── triggers ─────────────────────────────────────────────────────────────────
-- A trigger binds (source, event type, match condition) → (agent type, prompt
-- template). When a matching delivery arrives on the source, a run is started for
-- the target agent under the source's mapped identity, with the rendered prompt
-- as the run's initial message. grant_id references the policy grant that gives
-- the source principal the per-agent agents:runs:{slug} scopes.
CREATE TABLE triggers (
    id                  TEXT        PRIMARY KEY,
    tenant_id           TEXT        NOT NULL,
    source_id           TEXT        NOT NULL REFERENCES webhook_sources (id) ON DELETE CASCADE,
    name                TEXT        NOT NULL,
    agent_type_id       TEXT        NOT NULL,
    agent_slug          TEXT        NOT NULL,                 -- denormalized for run scope derivation
    event_type          TEXT        NOT NULL,                 -- e.g. "pull_request", "push"
    match_condition     JSONB       NOT NULL DEFAULT '{}'::jsonb, -- field -> allowed values, e.g. {"action":["opened"],"pr.base_ref":["main"]}
    prompt_template     TEXT        NOT NULL,
    grant_id            TEXT,                                 -- policy grant for agents:runs:{slug} scopes
    enabled             BOOLEAN     NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_triggers_tenant_agent ON triggers (tenant_id, agent_type_id);
CREATE INDEX idx_triggers_source_id ON triggers (source_id);

-- ─── trigger_deliveries ───────────────────────────────────────────────────────
-- A durable, queryable record of every inbound delivery and its processing
-- outcome — the audit/debug surface for the Triggers feature. Deliveries are NOT
-- foreign-keyed to sources/triggers so the history is retained after a source or
-- trigger is deleted. provider_delivery_id is used for redelivery dedup.
CREATE TABLE trigger_deliveries (
    id                    TEXT        PRIMARY KEY,
    tenant_id             TEXT        NOT NULL,
    source_id             TEXT        NOT NULL,
    provider_delivery_id  TEXT        NOT NULL DEFAULT '',    -- provider's delivery id (GitHub X-GitHub-Delivery)
    event_type            TEXT        NOT NULL DEFAULT '',
    status                TEXT        NOT NULL,               -- verified|dispatched|skipped|failed
    trigger_id            TEXT,                               -- matched trigger (nullable)
    run_id                TEXT,                               -- started run (nullable)
    error                 TEXT,
    payload_excerpt       TEXT        NOT NULL DEFAULT '',    -- truncated payload for debugging
    received_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_trigger_deliveries_tenant ON trigger_deliveries (tenant_id, received_at DESC);
CREATE INDEX idx_trigger_deliveries_source ON trigger_deliveries (source_id, received_at DESC);
CREATE INDEX idx_trigger_deliveries_trigger ON trigger_deliveries (trigger_id);

-- Dedup: a provider delivery id is processed at most once per source. The partial
-- index ignores empty ids (sources whose adapter does not supply a delivery id).
CREATE UNIQUE INDEX idx_trigger_deliveries_dedup
    ON trigger_deliveries (source_id, provider_delivery_id)
    WHERE provider_delivery_id <> '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS trigger_deliveries;
DROP TABLE IF EXISTS triggers;
DROP TABLE IF EXISTS webhook_sources;

-- +goose StatementEnd
