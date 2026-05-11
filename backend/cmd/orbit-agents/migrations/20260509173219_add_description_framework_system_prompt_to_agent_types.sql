-- +goose Up
-- +goose StatementBegin
ALTER TABLE agent_types
    ADD COLUMN description   TEXT NOT NULL DEFAULT '',
    ADD COLUMN system_prompt TEXT NOT NULL DEFAULT '',
    ADD COLUMN framework     TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types
    DROP COLUMN description,
    DROP COLUMN system_prompt,
    DROP COLUMN framework;
-- +goose StatementEnd
