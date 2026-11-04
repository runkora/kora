-- +goose Up
-- display_name is an environment's name for people, and name stays the slug agents select by
-- (docs/adr/2026-10-03-sandbox-environments.md, Amendment 1). An empty display_name reads as the slug.
-- +goose StatementBegin
ALTER TABLE environments ADD COLUMN display_name TEXT DEFAULT '' NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE environments SET display_name = name WHERE display_name = '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE environments DROP COLUMN IF EXISTS display_name;
-- +goose StatementEnd
