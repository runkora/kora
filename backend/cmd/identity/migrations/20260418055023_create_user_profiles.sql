-- +goose Up
-- +goose StatementBegin
CREATE TABLE user_profiles (
    id           TEXT        NOT NULL,
    tenant_id    TEXT        NOT NULL,
    email        TEXT        NOT NULL,
    display_name TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, tenant_id)
);

CREATE INDEX idx_user_profiles_tenant ON user_profiles (tenant_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_profiles;
-- +goose StatementEnd
