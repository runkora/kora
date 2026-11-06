-- +goose Up
-- The principal's display name at publish time, from koraprincipalname.
ALTER TABLE events ADD COLUMN principal_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE events DROP COLUMN IF EXISTS principal_name;
