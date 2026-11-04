-- +goose Up
-- +goose StatementBegin
-- W3C traceparent of the fire that queued the row; its dispatch and run join that trace.
ALTER TABLE trigger_pending_deliveries ADD COLUMN trace_parent TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE trigger_pending_deliveries DROP COLUMN trace_parent;
-- +goose StatementEnd
