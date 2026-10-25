-- +goose Up
-- +goose StatementBegin
-- A row projected from a TriggerSource or Trigger resource has source 'crd' and the resource's UID
-- (docs/adr/2026-10-24-declarative-resources.md). repository is the resource's runkora.io/repository
-- annotation, shown on the row.
ALTER TABLE webhook_sources
  ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
  ADD COLUMN resource_uid TEXT,
  ADD COLUMN repository TEXT;
ALTER TABLE triggers
  ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
  ADD COLUMN resource_uid TEXT,
  ADD COLUMN repository TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE triggers DROP COLUMN repository, DROP COLUMN resource_uid, DROP COLUMN source;
ALTER TABLE webhook_sources DROP COLUMN repository, DROP COLUMN resource_uid, DROP COLUMN source;
-- +goose StatementEnd
