-- +goose Up
-- outcome_counted records that the delivery's final outcome has been reported to
-- the delivery-outcomes metric, so the count is once per delivery however many
-- times its status is re-derived or pre-written.
-- +goose StatementBegin
ALTER TABLE trigger_deliveries
    ADD COLUMN outcome_counted BOOLEAN NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE trigger_deliveries DROP COLUMN outcome_counted;
-- +goose StatementEnd
