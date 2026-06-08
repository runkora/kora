-- +goose Up
-- +goose StatementBegin
CREATE INDEX ON principal_quota_group_memberships (group_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS principal_quota_group_memberships_group_id_idx;
-- +goose StatementEnd
