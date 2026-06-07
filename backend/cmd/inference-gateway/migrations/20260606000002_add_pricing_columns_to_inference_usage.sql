-- +goose Up
ALTER TABLE inference_usage
    ADD COLUMN price_unknown                     BOOLEAN        NOT NULL DEFAULT false,
    ADD COLUMN effective_input_cost_per_1m       NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN effective_output_cost_per_1m      NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN effective_cache_read_cost_per_1m  NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN effective_cache_write_cost_per_1m NUMERIC(10, 6) NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE inference_usage
    DROP COLUMN IF EXISTS price_unknown,
    DROP COLUMN IF EXISTS effective_input_cost_per_1m,
    DROP COLUMN IF EXISTS effective_output_cost_per_1m,
    DROP COLUMN IF EXISTS effective_cache_read_cost_per_1m,
    DROP COLUMN IF EXISTS effective_cache_write_cost_per_1m;
