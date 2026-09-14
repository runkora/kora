-- +goose Up
-- +goose StatementBegin
CREATE TABLE trigger_delivery_runs (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    run_id TEXT,
    status TEXT NOT NULL,
    error TEXT,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    trigger_name TEXT,
    CONSTRAINT trigger_delivery_runs_pkey PRIMARY KEY (id),
    CONSTRAINT trigger_delivery_runs_status_check CHECK (status IN ('dispatched', 'failed', 'filtered', 'denied', 'superseded', 'dropped', 'paused')),
    CONSTRAINT trigger_delivery_runs_delivery_id_fkey FOREIGN KEY (delivery_id) REFERENCES trigger_deliveries(id) ON DELETE CASCADE
);

CREATE INDEX idx_trigger_delivery_runs_delivery ON trigger_delivery_runs (delivery_id);

CREATE INDEX idx_trigger_delivery_runs_trigger ON trigger_delivery_runs (tenant_id, trigger_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS trigger_delivery_runs;
-- +goose StatementEnd
