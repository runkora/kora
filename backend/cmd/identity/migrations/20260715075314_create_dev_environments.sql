-- +goose Up
-- +goose StatementBegin

-- ─── dev_environments ─────────────────────────────────────────────────────────
-- The devenvironments feature's Postgres registry: ownership and audit trail
-- only — never lifecycle state (phase, ingress host, expiry, kubeconfig
-- material, ...), which is always read live from the OrbitDevEnvironment CR
-- the operator reconciles.
-- id doubles as the CR name and the host namespace suffix
-- ("orbit-devenv-{id}"). Soft-deleted (deleted_at) rather than removed, so
-- the audit trail survives destroy/expiry.
CREATE TABLE dev_environments (
    id          TEXT        PRIMARY KEY,
    owner       TEXT        NOT NULL,            -- verified platform token subject that provisioned this environment
    ref         TEXT        NOT NULL DEFAULT '',  -- chart/image ref installed inside the environment; '' means the CRD's own "main" default
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ                       -- soft-delete: set on explicit destroy or once the CR is observed gone (TTL expiry)
);

-- CountActiveByOwner (quota enforcement) and ListByOwner both filter on
-- (owner, deleted_at IS NULL) — a partial index keeps that check O(1)-ish
-- regardless of how many deleted rows accumulate.
CREATE INDEX idx_dev_environments_owner_active
    ON dev_environments (owner)
    WHERE deleted_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS dev_environments;

-- +goose StatementEnd
