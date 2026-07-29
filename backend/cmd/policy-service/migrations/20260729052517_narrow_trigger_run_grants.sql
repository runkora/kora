-- +goose Up
-- +goose StatementBegin
-- Narrow the run scopes already held by trigger and source principals.
--
-- Trigger and source principals used to be granted the unslugged
-- agents:runs:read/write alongside their per-agent grant. Because a parent
-- scope prefix-covers every child, that grant let a single trigger's token
-- start a run for ANY agent type in its tenant, defeating the per-agent
-- narrowing it was issued beside. Newly created principals no longer receive
-- it; this makes the fix retroactive for principals that already exist.
--
-- The per-agent grants live in their own rows and are untouched, so a trigger
-- keeps exactly the agent it binds. Only the over-broad row is reduced, and it
-- keeps agents:types:read, which a trigger genuinely needs to resolve its
-- target before starting a run.
--
-- Deliberately scoped to trigger-owned principals: humans and admin roles
-- legitimately hold agents:runs:write and are left alone.
--
-- Takes effect on the next bundle compile, not on commit. The policy service
-- recompiles a tenant's bundle when a grant is written THROUGH it, so rows
-- changed underneath it here are invisible to running enforcers until it
-- restarts. A normal deploy satisfies that, because the rollout that carries
-- this migration also replaces the policy-service pod; a migration applied
-- without one needs the pods restarted before the change is live.
-- Backfill first, narrow second, so no principal is ever without a run scope.
--
-- A trigger provisioned before the per-agent grant existed holds ONLY the broad
-- row: narrowing it alone would take away the last scope that let it start its
-- own agent, breaking a working trigger in the name of tightening it. Every
-- trigger records the agent it is bound to (triggers.agent_slug), and the
-- principal that holds its grants is its own for a self-owned kind or its
-- source's for a webhook, so the narrow grant it should have had is derivable
-- rather than guessed.
--
-- Reading triggers/webhook_sources from a policy-service migration is a
-- deliberate one-off: this repairs policy_grants rows, and the two services
-- share a database. It is a data repair, not a standing dependency.
INSERT INTO policy_grants (id, tenant_id, principal_type, principal_id, scopes)
SELECT gen_random_uuid()::text,
       t.tenant_id,
       'SERVICE',
       coalesce(t.principal_id, s.principal_id),
       jsonb_build_array('agents:runs:' || t.agent_slug || ':read',
                         'agents:runs:' || t.agent_slug || ':write')
FROM        triggers t
LEFT JOIN   webhook_sources s ON s.id = t.source_id
WHERE       coalesce(t.principal_id, s.principal_id) IS NOT NULL
  AND       t.agent_slug IS NOT NULL AND t.agent_slug <> ''
  AND  NOT EXISTS (
        SELECT 1 FROM policy_grants g
        WHERE  g.principal_id = coalesce(t.principal_id, s.principal_id)
          AND  g.scopes @> jsonb_build_array('agents:runs:' || t.agent_slug || ':write'))
GROUP BY    t.tenant_id, coalesce(t.principal_id, s.principal_id), t.agent_slug;

UPDATE policy_grants
SET    scopes = '["agents:types:read"]'::jsonb
WHERE  (principal_id LIKE 'trigger:%' OR principal_id LIKE 'trigger-source:%')
  AND  scopes @> '["agents:runs:write"]'::jsonb;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deliberately not reversible. Restoring the broad scopes would reopen the
-- privilege escalation this migration closes, and the pre-migration rows are
-- recoverable only as that over-broad form. A trigger missing a scope it needs
-- is repaired by recreating it, which mints the correct narrow set.
SELECT 1;
-- +goose StatementEnd
