-- +goose Up
-- +goose StatementBegin
-- The W3C traceparent of the fire that queued the row, so its dispatch and run join the fire's trace
-- (docs/adr/2026-10-31-one-trace-per-run.md).
ALTER TABLE trigger_pending_deliveries ADD COLUMN trace_parent TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE trigger_pending_deliveries DROP COLUMN trace_parent;
-- +goose StatementEnd
