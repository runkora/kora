-- +goose Up
-- +goose StatementBegin
-- An environment names its SandboxImage explicitly; kora-base is the base image.
ALTER TABLE environments ALTER COLUMN sandbox_image SET DEFAULT 'kora-base';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE environments ALTER COLUMN sandbox_image SET DEFAULT '';
-- +goose StatementEnd
