-- +goose Up
-- coalesce_key and dispatch_after hold a fire back until its coalescing window closes.
-- A NULL dispatch_after means the row is claimable at once.
-- +goose StatementBegin
ALTER TABLE trigger_pending_deliveries
    ADD COLUMN coalesce_key TEXT,
    ADD COLUMN dispatch_after TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_pending_coalesce ON trigger_pending_deliveries (trigger_id, coalesce_key)
    WHERE claimed_at IS NULL AND coalesce_key IS NOT NULL;
-- +goose StatementEnd

-- superseded_by names the delivery whose fire replaced this one.
-- +goose StatementBegin
ALTER TABLE trigger_delivery_runs ADD COLUMN superseded_by TEXT;
-- +goose StatementEnd

-- The rendered coalescing key of a delivery, per trigger. Written when the fire is enqueued.
-- +goose StatementBegin
CREATE TABLE trigger_delivery_keys (
    delivery_id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    coalesce_key TEXT,
    key_too_long BOOLEAN DEFAULT false NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT trigger_delivery_keys_pkey PRIMARY KEY (delivery_id, trigger_id),
    CONSTRAINT trigger_delivery_keys_delivery_id_fkey FOREIGN KEY (delivery_id) REFERENCES trigger_deliveries(id) ON DELETE CASCADE,
    CONSTRAINT trigger_delivery_keys_key_check CHECK ((coalesce_key IS NULL) = key_too_long),
    CONSTRAINT trigger_delivery_keys_length_check CHECK (coalesce_key IS NULL OR octet_length(coalesce_key) <= 256)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS trigger_delivery_keys;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE trigger_delivery_runs DROP COLUMN IF EXISTS superseded_by;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_pending_coalesce;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE trigger_pending_deliveries
    DROP COLUMN IF EXISTS dispatch_after,
    DROP COLUMN IF EXISTS coalesce_key;
-- +goose StatementEnd
