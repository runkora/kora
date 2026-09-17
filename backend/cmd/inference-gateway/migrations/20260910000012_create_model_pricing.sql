-- +goose Up
-- +goose StatementBegin
CREATE TABLE model_pricing (
    provider TEXT NOT NULL,
    model_id TEXT NOT NULL,
    synced_input_cost_per_1m NUMERIC(10,6) DEFAULT 0 NOT NULL,
    synced_output_cost_per_1m NUMERIC(10,6) DEFAULT 0 NOT NULL,
    synced_cache_read_cost_per_1m NUMERIC(10,6) DEFAULT 0 NOT NULL,
    synced_cache_write_cost_per_1m NUMERIC(10,6) DEFAULT 0 NOT NULL,
    override_input_cost_per_1m NUMERIC(10,6),
    override_output_cost_per_1m NUMERIC(10,6),
    override_cache_read_cost_per_1m NUMERIC(10,6),
    override_cache_write_cost_per_1m NUMERIC(10,6),
    last_synced_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT model_pricing_pkey PRIMARY KEY (provider, model_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS model_pricing;
-- +goose StatementEnd
