-- +goose Up
-- +goose StatementBegin
-- The kind of trigger that started a run. See docs/adr/2026-06-20-extended-trigger-types.md.
ALTER TABLE runs ADD COLUMN trigger_kind TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN trigger_kind;
-- +goose StatementEnd
