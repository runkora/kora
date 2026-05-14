-- +goose Up
CREATE TABLE user_preferences (
    user_id    TEXT        NOT NULL,
    tenant_id  TEXT        NOT NULL,
    prefs      JSONB       NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, tenant_id),
    CHECK (octet_length(prefs::text) <= 65536)
);

-- Supports tenant-wide administrative queries (ADR-0061).
CREATE INDEX idx_user_preferences_tenant ON user_preferences (tenant_id, user_id);

-- +goose Down
DROP TABLE user_preferences;
