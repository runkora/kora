-- +goose Up
-- +goose StatementBegin
-- identity_mode is the agent type's identity model (docs/adr/2026-07-23-delegated-agent-identity.md,
-- Decision 1): 'autonomous' agents act on their own grant; 'delegated' agents act
-- on behalf of the person who runs them. Immutable after creation (enforced in
-- the service layer); existing rows backfill to 'autonomous' via the default.
ALTER TABLE agent_types
    ADD COLUMN identity_mode TEXT NOT NULL DEFAULT 'autonomous'
    CHECK (identity_mode IN ('autonomous', 'delegated'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN identity_mode;
-- +goose StatementEnd
