-- +goose Up
ALTER TABLE user_pat_tokens
    ADD COLUMN scopes   JSONB NOT NULL DEFAULT '[]'::JSONB,
    ADD COLUMN grant_id TEXT;

-- +goose Down
ALTER TABLE user_pat_tokens
    DROP COLUMN scopes,
    DROP COLUMN grant_id;
