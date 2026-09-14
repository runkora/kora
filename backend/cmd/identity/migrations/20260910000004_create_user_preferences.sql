-- +goose Up
-- +goose StatementBegin
CREATE TABLE user_preferences (
    user_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    prefs JSONB DEFAULT '{}'::jsonb NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT user_preferences_pkey PRIMARY KEY (user_id, tenant_id),
    CONSTRAINT user_preferences_prefs_check CHECK (octet_length(prefs::text) <= 65536)
);

CREATE INDEX idx_user_preferences_tenant ON user_preferences (tenant_id, user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_preferences;
-- +goose StatementEnd
