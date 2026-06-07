-- +goose Up
CREATE INDEX ON quota_groups (tenant_id, name ASC);
CREATE INDEX ON principal_quota_group_memberships (group_id);

-- +goose Down
DROP INDEX IF EXISTS quota_groups_tenant_id_name_idx;
DROP INDEX IF EXISTS principal_quota_group_memberships_group_id_idx;
