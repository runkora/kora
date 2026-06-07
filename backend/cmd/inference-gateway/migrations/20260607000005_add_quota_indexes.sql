-- +goose Up
CREATE INDEX ON principal_quota_group_memberships (group_id);

-- +goose Down
DROP INDEX IF EXISTS principal_quota_group_memberships_group_id_idx;
