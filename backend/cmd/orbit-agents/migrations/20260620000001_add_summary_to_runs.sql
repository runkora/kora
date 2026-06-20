-- +goose Up
-- +goose StatementBegin

-- Add summary column to runs table.
-- Populated asynchronously at run start by the RunSummarizer when an initial
-- message is present. Null for runs with no initial message or when
-- summarization fails (best-effort; never blocks run creation).
ALTER TABLE runs ADD COLUMN summary TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE runs DROP COLUMN IF EXISTS summary;

-- +goose StatementEnd
