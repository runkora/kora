-- +goose Up
-- +goose StatementBegin
CREATE TABLE trigger_pending_deliveries (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    fields JSONB DEFAULT '{}'::jsonb NOT NULL,
    enqueued_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    claimed_at TIMESTAMPTZ,
    CONSTRAINT trigger_pending_deliveries_pkey PRIMARY KEY (id)
);

CREATE INDEX idx_pending_claim ON trigger_pending_deliveries (trigger_id, enqueued_at) WHERE claimed_at IS NULL;

CREATE INDEX idx_pending_claimed ON trigger_pending_deliveries (claimed_at) WHERE claimed_at IS NOT NULL;

CREATE INDEX idx_pending_delivery ON trigger_pending_deliveries (delivery_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS trigger_pending_deliveries;
-- +goose StatementEnd
