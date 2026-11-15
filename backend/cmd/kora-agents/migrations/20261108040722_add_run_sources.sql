-- +goose Up
-- +goose StatementBegin
-- Links to what started a run and to what later deliveries joined to it. See docs/adr/2026-11-07-run-sources.md.
ALTER TABLE runs ADD COLUMN sources JSONB NOT NULL DEFAULT '[]';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN sources;
-- +goose StatementEnd
