-- +goose Up
-- +goose StatementBegin

-- Note: this migration is future-dated (20260601) intentionally, to ensure it
-- runs after all existing migrations regardless of when it is applied.

-- Add token_id column for PAT-authenticated request attribution.
-- NULL for non-PAT requests. Both PAT-issued and OIDC-issued JWTs carry
-- an OrbitClaims.SessionID, but only the PAT value (the PAT record UUID)
-- is meaningful for usage attribution — OIDC session IDs are not stored here.
ALTER TABLE inference_usage
    ADD COLUMN IF NOT EXISTS token_id TEXT NULL;

-- Replace the single-column principal_id index with a compound index that
-- covers the primary query pattern: all usage for a tenant+principal sorted
-- by recency. The old index is a strict subset of this one — every query it
-- could serve is handled better by the compound index.
DROP INDEX IF EXISTS idx_inference_usage_principal_id;

CREATE INDEX IF NOT EXISTS idx_inference_usage_principal
    ON inference_usage (tenant_id, principal_id, created_at DESC);

-- Support filtering a specific PAT's usage within a tenant.
CREATE INDEX IF NOT EXISTS idx_inference_usage_token
    ON inference_usage (tenant_id, token_id, created_at DESC)
    WHERE token_id IS NOT NULL;

-- Extend the principal_type CHECK constraint to accept the PAT principal type.
-- The original constraint was created without an explicit name, so Postgres
-- auto-assigned the name inference_usage_principal_type_check.
-- Verify with: \d inference_usage  before running this migration.
ALTER TABLE inference_usage
    DROP CONSTRAINT inference_usage_principal_type_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_principal_type_check
        CHECK (principal_type IN ('AGENT', 'HUMAN', 'SERVICE', 'SYSTEM', 'PAT'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Revert principal_type CHECK constraint — remove PAT.
ALTER TABLE inference_usage
    DROP CONSTRAINT inference_usage_principal_type_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_principal_type_check
        CHECK (principal_type IN ('AGENT', 'HUMAN', 'SERVICE', 'SYSTEM'));

-- Drop token_id index before dropping the column.
DROP INDEX IF EXISTS idx_inference_usage_token;

-- Remove token_id column.
ALTER TABLE inference_usage
    DROP COLUMN IF EXISTS token_id;

-- Restore original single-column principal_id index and drop compound replacement.
DROP INDEX IF EXISTS idx_inference_usage_principal;

CREATE INDEX IF NOT EXISTS idx_inference_usage_principal_id
    ON inference_usage (principal_id, created_at DESC);

-- +goose StatementEnd
