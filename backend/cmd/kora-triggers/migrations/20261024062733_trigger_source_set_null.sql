-- +goose Up
-- Deleting a source keeps its triggers with no source. They stop firing and can only be deleted.
-- +goose StatementBegin
ALTER TABLE triggers DROP CONSTRAINT triggers_source_id_fkey;
ALTER TABLE triggers ADD CONSTRAINT triggers_source_id_fkey FOREIGN KEY (source_id) REFERENCES webhook_sources(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE triggers DROP CONSTRAINT triggers_source_id_fkey;
ALTER TABLE triggers ADD CONSTRAINT triggers_source_id_fkey FOREIGN KEY (source_id) REFERENCES webhook_sources(id) ON DELETE CASCADE;
-- +goose StatementEnd
