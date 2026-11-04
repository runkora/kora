-- +goose Up
-- +goose StatementBegin
-- A virtual model is authored through the API or projected from a VirtualModel resource (docs/adr/2026-10-24-declarative-resources.md).
-- A projected row records its resource's name and UID. One resource projects at most one row.
-- repository is the resource's runkora.io/repository annotation, shown on the row.
ALTER TABLE virtual_models
  ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
  ADD COLUMN resource_name TEXT,
  ADD COLUMN resource_uid TEXT,
  ADD COLUMN repository TEXT,
  ADD CONSTRAINT virtual_models_resource_check
    CHECK ((source = 'crd') = (resource_uid IS NOT NULL AND resource_name IS NOT NULL));
CREATE UNIQUE INDEX virtual_models_resource_uid_key ON virtual_models (resource_uid) WHERE resource_uid IS NOT NULL;
CREATE UNIQUE INDEX virtual_models_resource_name_key ON virtual_models (resource_name) WHERE resource_name IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX virtual_models_resource_name_key;
DROP INDEX virtual_models_resource_uid_key;
ALTER TABLE virtual_models
  DROP CONSTRAINT virtual_models_resource_check,
  DROP COLUMN repository,
  DROP COLUMN resource_uid,
  DROP COLUMN resource_name,
  DROP COLUMN source;
-- +goose StatementEnd
