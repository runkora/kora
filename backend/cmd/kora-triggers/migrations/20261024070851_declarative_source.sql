-- +goose Up
-- +goose StatementBegin
-- A row projected from a TriggerSource or Trigger resource has source 'crd' and the resource's UID
-- (docs/adr/2026-10-24-declarative-resources.md).
ALTER TABLE webhook_sources
  ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
  ADD COLUMN resource_uid TEXT;
ALTER TABLE triggers
  ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
  ADD COLUMN resource_uid TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE triggers DROP COLUMN resource_uid, DROP COLUMN source;
ALTER TABLE webhook_sources DROP COLUMN resource_uid, DROP COLUMN source;
-- +goose StatementEnd
