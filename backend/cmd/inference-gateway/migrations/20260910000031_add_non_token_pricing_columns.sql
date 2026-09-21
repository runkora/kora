-- +goose Up
-- +goose StatementBegin
ALTER TABLE model_pricing
    ADD COLUMN IF NOT EXISTS synced_image_input_cost_per_image  NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS synced_image_output_cost_per_image NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS synced_request_input_cost          NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS synced_request_output_cost         NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS synced_second_input_cost           NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS synced_second_output_cost          NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS synced_audio_input_cost_per_1m     NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS synced_audio_output_cost_per_1m    NUMERIC(10, 6) NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE model_pricing
    DROP COLUMN IF EXISTS synced_image_input_cost_per_image,
    DROP COLUMN IF EXISTS synced_image_output_cost_per_image,
    DROP COLUMN IF EXISTS synced_request_input_cost,
    DROP COLUMN IF EXISTS synced_request_output_cost,
    DROP COLUMN IF EXISTS synced_second_input_cost,
    DROP COLUMN IF EXISTS synced_second_output_cost,
    DROP COLUMN IF EXISTS synced_audio_input_cost_per_1m,
    DROP COLUMN IF EXISTS synced_audio_output_cost_per_1m;
-- +goose StatementEnd
