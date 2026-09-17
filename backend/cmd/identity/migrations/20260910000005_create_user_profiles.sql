-- +goose Up
-- +goose StatementBegin
CREATE TABLE user_profiles (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    email TEXT NOT NULL,
    display_name TEXT DEFAULT '' NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    roles TEXT[] DEFAULT '{}' NOT NULL,
    CONSTRAINT user_profiles_pkey PRIMARY KEY (id, tenant_id)
);

CREATE INDEX idx_user_profiles_roles ON user_profiles USING gin (roles);

CREATE INDEX idx_user_profiles_tenant ON user_profiles (tenant_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_profiles;
-- +goose StatementEnd
