-- +goose Up
-- +goose StatementBegin
-- W3C traceparent of the span that created the run; later steps join its trace.
ALTER TABLE runs ADD COLUMN trace_parent TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN trace_parent;
-- +goose StatementEnd
