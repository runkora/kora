-- +goose Up
-- +goose StatementBegin
-- delegation_jti records the delegation issuance minted for a delegated run at
-- start (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 3). NULL for
-- autonomous runs. orbit-agents revokes the issuance by this JTI when the run
-- reaches a terminal state — the delegation ends with the run.
ALTER TABLE runs
    ADD COLUMN delegation_jti TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN delegation_jti;
-- +goose StatementEnd
