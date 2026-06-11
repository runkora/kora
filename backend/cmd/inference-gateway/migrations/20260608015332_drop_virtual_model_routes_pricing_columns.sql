-- +goose Up
-- +goose StatementBegin
-- Remove the pricing columns from virtual_model_routes now that all pricing
-- is managed through the model_pricing catalog table.
ALTER TABLE virtual_model_routes
    DROP COLUMN IF EXISTS input_cost_per_1m,
    DROP COLUMN IF EXISTS output_cost_per_1m;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE virtual_model_routes
    ADD COLUMN input_cost_per_1m  NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN output_cost_per_1m NUMERIC(10, 6) NOT NULL DEFAULT 0;
-- +goose StatementEnd
