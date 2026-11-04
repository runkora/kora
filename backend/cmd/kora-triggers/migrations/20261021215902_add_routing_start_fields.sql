-- +goose Up
-- The rendered fields of the delivery that started a run, kept so the run question can label the run with them.
-- +goose StatementBegin
ALTER TABLE trigger_routing_decisions ADD COLUMN start_fields JSONB;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE trigger_routing_decisions DROP COLUMN IF EXISTS start_fields;
-- +goose StatementEnd
