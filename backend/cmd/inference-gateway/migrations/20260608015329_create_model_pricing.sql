-- +goose Up
-- +goose StatementBegin
CREATE TABLE model_pricing (
    provider                         TEXT           NOT NULL,
    model_id                         TEXT           NOT NULL,
    synced_input_cost_per_1m         NUMERIC(10, 6) NOT NULL DEFAULT 0,
    synced_output_cost_per_1m        NUMERIC(10, 6) NOT NULL DEFAULT 0,
    synced_cache_read_cost_per_1m    NUMERIC(10, 6) NOT NULL DEFAULT 0,
    synced_cache_write_cost_per_1m   NUMERIC(10, 6) NOT NULL DEFAULT 0,
    override_input_cost_per_1m       NUMERIC(10, 6),
    override_output_cost_per_1m      NUMERIC(10, 6),
    override_cache_read_cost_per_1m  NUMERIC(10, 6),
    override_cache_write_cost_per_1m NUMERIC(10, 6),
    last_synced_at                   TIMESTAMPTZ    NOT NULL,
    created_at                       TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at                       TIMESTAMPTZ    NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, model_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS model_pricing;
-- +goose StatementEnd
