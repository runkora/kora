-- +goose Up
-- +goose StatementBegin

-- run_id: the agent run a request belonged to (NULL for non-run traffic).
-- Source of truth for per-run budget counter rehydration and per-run cost audit.
-- principal_roles: the caller's IDP-asserted OIDC roles at request time.
-- Source of truth for role-pool counter rehydration (role membership is
-- request-time knowledge and otherwise unrecoverable).
-- See docs/adr/2026-06-10-hierarchical-budget-enforcement.md.
ALTER TABLE inference_usage ADD COLUMN IF NOT EXISTS run_id TEXT NULL;
ALTER TABLE inference_usage ADD COLUMN IF NOT EXISTS principal_roles TEXT[] NULL;

CREATE INDEX IF NOT EXISTS idx_inference_usage_run_id
    ON inference_usage (run_id) WHERE run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_inference_usage_principal_roles
    ON inference_usage USING GIN (principal_roles) WHERE principal_roles IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_inference_usage_principal_roles;
DROP INDEX IF EXISTS idx_inference_usage_run_id;
ALTER TABLE inference_usage DROP COLUMN IF EXISTS principal_roles;
ALTER TABLE inference_usage DROP COLUMN IF EXISTS run_id;

-- +goose StatementEnd
