-- +goose Up
-- +goose StatementBegin
-- delegation_required is the fail-closed marker for delegated agents
-- (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 2): a grant carrying
-- it stops being an entitlement on its own — the autonomous rego allow paths
-- refuse any principal holding such a grant, so a delegated agent without valid
-- delegation context can do nothing. Defaults false: existing grants unchanged.
ALTER TABLE policy_grants
    ADD COLUMN delegation_required BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE policy_grants DROP COLUMN delegation_required;
-- +goose StatementEnd
