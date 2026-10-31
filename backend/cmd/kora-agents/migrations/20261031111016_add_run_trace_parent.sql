-- +goose Up
-- +goose StatementBegin
-- The W3C traceparent of the span that created the run, so every later step joins the run's trace
-- (docs/adr/2026-10-31-one-trace-per-run.md).
ALTER TABLE runs ADD COLUMN trace_parent TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN trace_parent;
-- +goose StatementEnd
