-- +goose Up
-- +goose StatementBegin
-- A row projected from an Agent or Environment resource has source 'crd', the resource's UID
-- and the resource generation last applied (docs/adr/2026-10-24-declarative-resources.md).
ALTER TABLE agent_types
  ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
  ADD COLUMN resource_uid TEXT,
  ADD COLUMN resource_generation BIGINT NOT NULL DEFAULT 0;
ALTER TABLE environments
  ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
  ADD COLUMN resource_uid TEXT,
  ADD COLUMN resource_generation BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE environments DROP COLUMN resource_generation, DROP COLUMN resource_uid, DROP COLUMN source;
ALTER TABLE agent_types DROP COLUMN resource_generation, DROP COLUMN resource_uid, DROP COLUMN source;
-- +goose StatementEnd
