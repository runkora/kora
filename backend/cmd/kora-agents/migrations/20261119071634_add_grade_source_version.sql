-- +goose Up
-- +goose StatementBegin
-- The version whose grade a regrade or recompute read. NULL on a run's first grade.
ALTER TABLE grades ADD COLUMN source_version TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE grades DROP COLUMN source_version;
-- +goose StatementEnd
