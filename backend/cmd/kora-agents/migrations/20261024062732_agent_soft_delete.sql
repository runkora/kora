-- +goose Up
-- +goose StatementBegin
-- A deleted agent keeps its row so its runs keep their history (docs/adr/2026-10-24-declarative-resources.md).
-- The slug stays unique among live agents only. A new agent may reuse a deleted agent's slug.
ALTER TABLE agent_types ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE agent_types DROP CONSTRAINT agent_types_state_check;
ALTER TABLE agent_types ADD CONSTRAINT agent_types_state_check
  CHECK (state IN ('provisioning', 'active', 'deleting', 'deleted'));
ALTER TABLE agent_types DROP CONSTRAINT agent_types_tenant_id_slug_key;
CREATE UNIQUE INDEX agent_types_tenant_slug_live ON agent_types (tenant_id, slug) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX agent_types_tenant_slug_live;
ALTER TABLE agent_types ADD CONSTRAINT agent_types_tenant_id_slug_key UNIQUE (tenant_id, slug);
ALTER TABLE agent_types DROP CONSTRAINT agent_types_state_check;
ALTER TABLE agent_types ADD CONSTRAINT agent_types_state_check
  CHECK (state IN ('provisioning', 'active', 'deleting'));
ALTER TABLE agent_types DROP COLUMN deleted_at;
-- +goose StatementEnd
