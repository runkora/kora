-- +goose Up
-- +goose StatementBegin
-- Persist the principal's IdP-asserted roles (merged roles + groups) on every
-- login. This is the single source of truth for role membership: PATs and agent
-- tokens carry no roles claim, so role-budget attribution and enforcement for
-- non-interactive inference resolve membership from this snapshot rather than
-- from the request credential. See
-- docs/adr/2026-06-15-role-budget-attribution-for-non-interactive-credentials.md
ALTER TABLE user_profiles
    ADD COLUMN roles TEXT[] NOT NULL DEFAULT '{}';

-- GIN index supports "which principals currently hold role X" lookups used by
-- the inference gateway when rehydrating pooled-role spend.
CREATE INDEX idx_user_profiles_roles ON user_profiles USING GIN (roles);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_user_profiles_roles;
ALTER TABLE user_profiles
    DROP COLUMN IF EXISTS roles;
-- +goose StatementEnd
