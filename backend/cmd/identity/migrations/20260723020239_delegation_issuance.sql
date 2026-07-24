-- +goose Up
-- +goose StatementBegin
-- Delegation issuance records (docs/adr/2026-07-23-delegated-agent-identity.md,
-- Decision 3). One row per minted delegation: who delegated, to which agent,
-- for which run. The signed token is never stored — GET /identity/delegation/run/{run_id}
-- re-signs a fresh JWT from this record on every retrieval, so a tombstoned
-- (revoked_at) row yields nothing and fails closed.
CREATE TABLE delegation_issuances (
    -- jti identifies the delegation across every re-signed token; revocation
    -- blocklists this value.
    jti TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    -- The delegating principal (platform token sub) and its type (HUMAN or PAT-as-person).
    delegator_principal_id TEXT NOT NULL,
    delegator_principal_type TEXT NOT NULL,
    -- PAT lineage: set when the delegation was minted with a PAT platform token,
    -- so revoking the PAT can tombstone its issuances.
    delegator_pat_id TEXT,
    -- The agent the delegation names (act.sub) — the agent's SPIFFE URI.
    agent_principal_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Tombstone: a revoked issuance is kept as the audit record of the
    -- delegation event but is never released or re-signed.
    revoked_at TIMESTAMPTZ
);
-- +goose StatementEnd

-- +goose StatementBegin
-- At most one active (non-revoked) delegation per run.
CREATE UNIQUE INDEX delegation_issuances_active_run_idx
    ON delegation_issuances (run_id)
    WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
-- Delegator view: list a principal's active issuances.
CREATE INDEX delegation_issuances_delegator_idx
    ON delegation_issuances (tenant_id, delegator_principal_id)
    WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE delegation_issuances;
-- +goose StatementEnd
