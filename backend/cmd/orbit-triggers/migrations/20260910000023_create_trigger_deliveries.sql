-- +goose Up
-- +goose StatementBegin
CREATE TABLE trigger_deliveries (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    source_id TEXT,
    provider_delivery_id TEXT DEFAULT '' NOT NULL,
    event_type TEXT DEFAULT '' NOT NULL,
    status TEXT NOT NULL,
    trigger_id TEXT,
    run_id TEXT,
    error TEXT,
    payload_excerpt TEXT DEFAULT '' NOT NULL,
    received_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    fired_by TEXT,
    scheduled_at TIMESTAMPTZ,
    kind TEXT DEFAULT 'webhook' NOT NULL,
    payload BYTEA,
    payload_bytes INTEGER,
    payload_purged BOOLEAN DEFAULT false NOT NULL,
    reason TEXT,
    redelivery_of TEXT,
    trigger_name TEXT,
    CONSTRAINT trigger_deliveries_pkey PRIMARY KEY (id),
    CONSTRAINT trigger_deliveries_status_check CHECK (status IN ('verified', 'dispatched', 'skipped', 'failed', 'duplicate', 'missed'))
);

CREATE UNIQUE INDEX idx_trigger_deliveries_dedup ON trigger_deliveries (source_id, provider_delivery_id) WHERE ((provider_delivery_id <> '') AND (redelivery_of IS NULL));

CREATE INDEX idx_trigger_deliveries_purge ON trigger_deliveries (received_at) WHERE (payload IS NOT NULL);

CREATE INDEX idx_trigger_deliveries_redelivery_of ON trigger_deliveries (redelivery_of) WHERE (redelivery_of IS NOT NULL);

CREATE UNIQUE INDEX idx_trigger_deliveries_scheduled_dedup ON trigger_deliveries (trigger_id, scheduled_at) WHERE (scheduled_at IS NOT NULL);

CREATE INDEX idx_trigger_deliveries_source ON trigger_deliveries (source_id, received_at DESC);

CREATE INDEX idx_trigger_deliveries_tenant ON trigger_deliveries (tenant_id, received_at DESC);

CREATE INDEX idx_trigger_deliveries_trigger ON trigger_deliveries (trigger_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS trigger_deliveries;
-- +goose StatementEnd
