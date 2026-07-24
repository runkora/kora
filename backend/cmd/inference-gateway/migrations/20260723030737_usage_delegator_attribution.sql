-- +goose Up
-- +goose StatementBegin

-- delegator_id / delegator_principal_type: the person a delegated agent run
-- acted on behalf of, stamped by the inference gateway from the VERIFIED
-- delegation context — never from anything the caller asserts. NULL for all
-- non-delegated traffic. Spend rolls up to the person as well as the agent:
-- the delegator's budget windows accumulate from these rows, and the person's
-- analytics/usage views include rows where delegator_id = their principal id.
-- See docs/adr/2026-07-23-delegated-agent-identity.md (Decision 5).
ALTER TABLE inference_usage ADD COLUMN IF NOT EXISTS delegator_id TEXT NULL;
ALTER TABLE inference_usage ADD COLUMN IF NOT EXISTS delegator_principal_type TEXT NULL;

-- Serves the delegator-side window rehydration (spend since period start) and
-- the personal analytics rollup, both keyed (tenant_id, delegator_id, time).
CREATE INDEX IF NOT EXISTS idx_inference_usage_delegator
    ON inference_usage (tenant_id, delegator_id, created_at)
    WHERE delegator_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_inference_usage_delegator;
ALTER TABLE inference_usage DROP COLUMN IF EXISTS delegator_principal_type;
ALTER TABLE inference_usage DROP COLUMN IF EXISTS delegator_id;

-- +goose StatementEnd
