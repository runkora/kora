/**
 * @tier 3 — events-scoped story and test data. A tenant's last six hours of platform events in the
 * read API's envelope: runs and their sandboxes, a trigger whose run failed and the event
 * trigger that reacted to it, budgets running out, sign-ins and tokens, admin edits and a
 * declared resource that would not apply. No payload carries a token, secret or credential.
 */

import type { EventPage, EventPrincipalType, KoraEvent } from '../_types';

const TENANT = 'kora-tenant-example';

const agentSubject = (slug: string) => `spiffe://runkora.io/tenant/${TENANT}/agent/${slug}`;

interface StoryPrincipal {
  subject: string;
  type: EventPrincipalType;
  name: string;
}

const PRINCIPALS: StoryPrincipal[] = [
  { subject: 'usr_42b7e9', type: 'USER', name: 'Dev Operator' },
  { subject: 'usr_8c1f20', type: 'USER', name: 'Priya Raman' },
  { subject: 'usr_3d9a51', type: 'USER', name: 'Sam Okafor' },
  { subject: agentSubject('triage'), type: 'AGENT', name: 'triage' },
  { subject: agentSubject('pr-reviewer'), type: 'AGENT', name: 'pr-reviewer' },
  { subject: agentSubject('cost-sweeper'), type: 'AGENT', name: 'cost-sweeper' },
  { subject: agentSubject('release-notes'), type: 'AGENT', name: 'release-notes' },
  { subject: agentSubject('incident-notifier'), type: 'AGENT', name: 'incident-notifier' },
  { subject: 'trigger:trg_01JC7Q2M', type: 'TRIGGER', name: 'sentry-new-issue-triage' },
  { subject: 'trigger:trg_01JD0A1X', type: 'TRIGGER', name: 'nightly-dependency-audit' },
  { subject: 'trigger:trg_01JE4N8R', type: 'TRIGGER', name: 'on-triage-failed-notify' },
  { subject: 'kora-agents', type: 'SYSTEM', name: 'kora-agents' },
  { subject: 'kora-triggers', type: 'SYSTEM', name: 'kora-triggers' },
  {
    subject: 'inference-gateway',
    type: 'SYSTEM',
    name: 'inference-gateway',
  },
  { subject: 'operator', type: 'SYSTEM', name: 'operator' },
];

const who = (name: string): StoryPrincipal => {
  const p = PRINCIPALS.find((x) => x.name === name);
  if (p === undefined) throw new Error(`unknown principal ${name}`);
  return p;
};

const RUN = {
  failedTriage: '2b4e8c1d-5f7a-4c2e-9b1d-0e6f3a8c7d21',
  notifier: '7c1d9e40-2a6b-4f13-8e5c-91b0d2f4a6e3',
  review: '5e0a3b7f-c812-4d69-a1f4-3b7e9c20d815',
  sweep: '91f2c6a8-04d3-4b7e-b5a9-6c8e1f3d2a47',
  notes: 'c43a8e15-7b20-49d6-8f31-2e5d0c9b7a16',
  replaced: 'e8b17d42-3c95-4a60-9d2e-7f1a4b8c3e59',
  idleOut: '0d6f2a91-8e47-4c3b-a1d5-5b9e7c0f2d38',
} as const;

/** A UUIDv7-shaped id: the time in the first 48 bits, then a counter, so ids sort by time. */
function uuid7(ms: number, n: number): string {
  const t = Math.floor(ms).toString(16).padStart(12, '0');
  const c = n.toString(16).padStart(15, '0');
  return `${t.slice(0, 8)}-${t.slice(8, 12)}-7${c.slice(0, 3)}-9${c.slice(3, 6)}-${c.slice(6, 15)}abc`;
}

function traceparent(n: number): string {
  const trace = (0x4bf92f35 + n * 7919).toString(16).padStart(8, '0');
  return `00-${trace}77b34da6a3ce929d0e0e47${n.toString(16).padStart(2, '0')}-00f067aa0ba902b7-01`;
}

interface Spec {
  /** Minutes before `now`. */
  ago: number;
  type: string;
  by: string;
  subject: string;
  entity: Record<string, unknown>;
  changed?: string[];
  version?: number;
  run?: string;
  agent?: string;
  delegator?: string;
  /** Index into the spec list of the event that caused this one. */
  causedBy?: number;
  depth?: number;
  triggers?: string[];
  source?: string;
  /** Events with no trace leave it out. */
  noTrace?: boolean;
}

const SERVICE_OF: Record<string, string> = {
  agents: 'kora-agents',
  triggers: 'kora-triggers',
  inference: 'inference-gateway',
  identity: 'identity',
  policy: 'policy-service',
  declarative: 'operator',
};

const FAILED_RUN_EVENT = 8;

const SPECS: Spec[] = [
  {
    ago: 1,
    type: 'agents.run.completed.v1',
    by: 'incident-notifier',
    subject: `runs/${RUN.notifier}`,
    entity: { id: RUN.notifier, state: 'completed', turns: 2, cost_usd: 0.04 },
    changed: ['state'],
    version: 5,
    run: RUN.notifier,
    agent: 'incident-notifier',
    causedBy: FAILED_RUN_EVENT,
    depth: 1,
    triggers: ['trg_01JE4N8R'],
  },
  {
    ago: 2,
    type: 'inference.budget.threshold_crossed.v1',
    by: 'inference-gateway',
    subject: 'budgets/bud_cost-sweeper-monthly',
    entity: {
      id: 'bud_cost-sweeper-monthly',
      name: 'cost-sweeper monthly',
      level: 80,
      limit_usd: 200,
      spent_usd: 161.4,
      period: '2026-11',
    },
    agent: 'cost-sweeper',
  },
  {
    ago: 3,
    type: 'policy.grant.updated.v1',
    by: 'Dev Operator',
    subject: 'grants/grt_7f3a91c4',
    entity: {
      id: 'grt_7f3a91c4',
      name: 'triage-github-write',
      principal: 'agent:triage',
      scopes: ['mcp:github:issues:write', 'mcp:github:pulls:read', 'mcp:sentry:issues:read'],
      budget_usd: 50,
      expires_at: '2027-02-01T00:00:00Z',
    },
    changed: ['scopes', 'budget_usd'],
    version: 4,
    noTrace: false,
  },
  {
    ago: 4,
    type: 'agents.run.started.v1',
    by: 'incident-notifier',
    subject: `runs/${RUN.notifier}`,
    entity: { id: RUN.notifier, state: 'running' },
    changed: ['state'],
    version: 3,
    run: RUN.notifier,
    agent: 'incident-notifier',
    causedBy: FAILED_RUN_EVENT,
    depth: 1,
    triggers: ['trg_01JE4N8R'],
  },
  {
    ago: 4.2,
    type: 'agents.sandbox.ready.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.notifier}/sandbox`,
    entity: {
      id: `sbx_${RUN.notifier.slice(0, 8)}`,
      environment: 'python-3-12',
      image: 'ghcr.io/example-org/agent-base:2.3',
      egress: 'passed',
    },
    run: RUN.notifier,
    agent: 'incident-notifier',
    causedBy: FAILED_RUN_EVENT,
    depth: 1,
    triggers: ['trg_01JE4N8R'],
  },
  {
    ago: 4.8,
    type: 'agents.sandbox.created.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.notifier}/sandbox`,
    entity: { id: `sbx_${RUN.notifier.slice(0, 8)}`, environment: 'python-3-12', size: 'small' },
    run: RUN.notifier,
    agent: 'incident-notifier',
    causedBy: FAILED_RUN_EVENT,
    depth: 1,
    triggers: ['trg_01JE4N8R'],
  },
  {
    ago: 5,
    type: 'agents.run.created.v1',
    by: 'on-triage-failed-notify',
    subject: `runs/${RUN.notifier}`,
    entity: { id: RUN.notifier, state: 'queued', agent: 'incident-notifier' },
    version: 1,
    run: RUN.notifier,
    agent: 'incident-notifier',
    causedBy: FAILED_RUN_EVENT,
    depth: 1,
    triggers: ['trg_01JE4N8R'],
  },
  {
    ago: 6,
    type: 'triggers.delivery.failed.v1',
    by: 'sentry-new-issue-triage',
    subject: 'deliveries/dlv_920ff1e7',
    entity: {
      id: 'dlr_5d20ab31',
      delivery_id: 'dlv_920ff1e7',
      trigger_id: 'trg_01JC7Q2M',
      trigger_name: 'sentry-new-issue-triage',
      event_type: 'issue.created',
      error: 'sandbox pool exhausted',
    },
    agent: 'triage',
  },
  {
    ago: 7,
    type: 'agents.run.failed.v1',
    by: 'triage',
    subject: `runs/${RUN.failedTriage}`,
    entity: { id: RUN.failedTriage, state: 'failed', error: 'sandbox_failed' },
    changed: ['state', 'error'],
    version: 7,
    run: RUN.failedTriage,
    agent: 'triage',
    triggers: ['trg_01JC7Q2M'],
  },
  {
    ago: 7.3,
    type: 'agents.sandbox.failed.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.failedTriage}/sandbox`,
    entity: {
      id: `sbx_${RUN.failedTriage.slice(0, 8)}`,
      environment: 'triage-node-22',
      image: 'ghcr.io/example-org/triage-sandbox:1.4',
      error: 'image pull back-off',
    },
    run: RUN.failedTriage,
    agent: 'triage',
  },
  {
    ago: 8,
    type: 'agents.sandbox.created.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.failedTriage}/sandbox`,
    entity: { id: `sbx_${RUN.failedTriage.slice(0, 8)}`, environment: 'triage-node-22' },
    run: RUN.failedTriage,
    agent: 'triage',
  },
  {
    ago: 8.2,
    type: 'agents.run.created.v1',
    by: 'sentry-new-issue-triage',
    subject: `runs/${RUN.failedTriage}`,
    entity: { id: RUN.failedTriage, state: 'queued', agent: 'triage' },
    version: 1,
    run: RUN.failedTriage,
    agent: 'triage',
    triggers: ['trg_01JC7Q2M'],
  },
  {
    ago: 11,
    type: 'agents.run.guardrail_warned.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.review}`,
    entity: { id: RUN.review, repeated_tool: 'github.get_file', repeats: 5 },
    run: RUN.review,
    agent: 'pr-reviewer',
    delegator: 'usr_8c1f20',
  },
  {
    ago: 12,
    type: 'agents.run.message_received.v1',
    by: 'Priya Raman',
    subject: `runs/${RUN.review}`,
    entity: { id: RUN.review, from: 'usr_8c1f20', length: 184 },
    run: RUN.review,
    agent: 'pr-reviewer',
  },
  {
    ago: 14,
    type: 'agents.run.idle.v1',
    by: 'pr-reviewer',
    subject: `runs/${RUN.review}`,
    entity: { id: RUN.review, state: 'idle', turns: 1 },
    changed: ['state'],
    version: 4,
    run: RUN.review,
    agent: 'pr-reviewer',
    delegator: 'usr_8c1f20',
  },
  {
    ago: 16,
    type: 'agents.run.started.v1',
    by: 'pr-reviewer',
    subject: `runs/${RUN.review}`,
    entity: { id: RUN.review, state: 'running' },
    changed: ['state'],
    version: 2,
    run: RUN.review,
    agent: 'pr-reviewer',
    delegator: 'usr_8c1f20',
  },
  {
    ago: 16.4,
    type: 'identity.delegation.issued.v1',
    by: 'Priya Raman',
    subject: 'delegations/dlg_5a2e81',
    entity: {
      id: 'dlg_5a2e81',
      agent: 'pr-reviewer',
      scopes: ['mcp:github:pulls:read', 'mcp:github:pulls:comment'],
      expires_at: '2026-11-06T18:00:00Z',
    },
    agent: 'pr-reviewer',
  },
  {
    ago: 17,
    type: 'agents.run.created.v1',
    by: 'Priya Raman',
    subject: `runs/${RUN.review}`,
    entity: { id: RUN.review, state: 'queued', agent: 'pr-reviewer' },
    version: 1,
    run: RUN.review,
    agent: 'pr-reviewer',
  },
  {
    ago: 18,
    type: 'identity.session.created.v1',
    by: 'Priya Raman',
    subject: 'sessions/ses_9b04c2',
    entity: { id: 'ses_9b04c2', method: 'oidc', client: 'dashboard' },
  },
  {
    ago: 22,
    type: 'triggers.trigger.autopaused.v1',
    by: 'kora-triggers',
    subject: 'triggers/trg_01JD0A1X',
    entity: {
      id: 'trg_01JD0A1X',
      name: 'nightly-dependency-audit',
      enabled: false,
      consecutive_failures: 10,
    },
    changed: ['enabled'],
    version: 12,
    agent: 'dependency-auditor',
  },
  {
    ago: 23,
    type: 'triggers.delivery.denied.v1',
    by: 'nightly-dependency-audit',
    subject: 'deliveries/dlv_4c81aa02',
    entity: {
      id: 'dlr_8a07c4e2',
      delivery_id: 'dlv_4c81aa02',
      trigger_id: 'trg_01JD0A1X',
      trigger_name: 'nightly-dependency-audit',
      missing_scope: 'agents:run:dependency-auditor',
    },
    agent: 'dependency-auditor',
  },
  {
    ago: 35,
    type: 'agents.run.budget_exhausted.v1',
    by: 'inference-gateway',
    subject: `runs/${RUN.sweep}`,
    entity: { id: RUN.sweep, state: 'failed', budget: 'cost-sweeper daily' },
    changed: ['state'],
    version: 9,
    run: RUN.sweep,
    agent: 'cost-sweeper',
  },
  {
    ago: 36,
    type: 'inference.budget.exceeded.v1',
    by: 'inference-gateway',
    subject: 'budgets/bud_cost-sweeper-daily',
    entity: {
      id: 'bud_cost-sweeper-daily',
      name: 'cost-sweeper daily',
      model: 'bedrock/claude-sonnet-4-5',
      limit_usd: 20,
      spent_usd: 20.07,
    },
    run: RUN.sweep,
    agent: 'cost-sweeper',
  },
  {
    ago: 38,
    type: 'inference.budget.threshold_crossed.v1',
    by: 'inference-gateway',
    subject: 'budgets/bud_cost-sweeper-daily',
    entity: {
      id: 'bud_cost-sweeper-daily',
      name: 'cost-sweeper daily',
      level: 100,
      limit_usd: 20,
      spent_usd: 20.01,
      period: '2026-11-06',
    },
    run: RUN.sweep,
    agent: 'cost-sweeper',
  },
  {
    ago: 52,
    type: 'agents.run.terminated.v1',
    by: 'Sam Okafor',
    subject: `runs/${RUN.notes}`,
    entity: { id: RUN.notes, state: 'terminated', reason: 'terminated by user' },
    changed: ['state'],
    version: 8,
    run: RUN.notes,
    agent: 'release-notes',
  },
  {
    ago: 55,
    type: 'agents.run.interrupted.v1',
    by: 'Sam Okafor',
    subject: `runs/${RUN.notes}`,
    entity: { id: RUN.notes, turn: 3 },
    run: RUN.notes,
    agent: 'release-notes',
  },
  {
    ago: 58,
    type: 'agents.run.errored.v1',
    by: 'release-notes',
    subject: `runs/${RUN.notes}`,
    entity: { id: RUN.notes, error: 'tool call timed out: github.list_pull_requests', turn: 2 },
    run: RUN.notes,
    agent: 'release-notes',
    delegator: 'usr_3d9a51',
  },
  {
    ago: 70,
    type: 'agents.environment.updated.v1',
    by: 'Dev Operator',
    subject: 'environments/env_python-3-12',
    entity: {
      id: 'env_python-3-12',
      name: 'python-3-12',
      image: 'ghcr.io/example-org/agent-base:2.3',
      size: 'medium',
      egress: ['github.com', 'pypi.org', 'files.pythonhosted.org'],
    },
    changed: ['image'],
    version: 6,
  },
  {
    ago: 72,
    type: 'agents.sandbox.replaced.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.replaced}/sandbox`,
    entity: {
      id: `sbx_${RUN.replaced.slice(0, 8)}`,
      from_image: 'ghcr.io/example-org/agent-base:2.2',
      to_image: 'ghcr.io/example-org/agent-base:2.3',
    },
    run: RUN.replaced,
    agent: 'triage',
  },
  {
    ago: 80,
    type: 'identity.pat.created.v1',
    by: 'Dev Operator',
    subject: 'pats/pat_61c0f9',
    entity: {
      id: 'pat_61c0f9',
      name: 'ci-deploy',
      scopes: ['agents:read', 'triggers:fire'],
      expires_at: '2027-05-06T00:00:00Z',
    },
  },
  {
    ago: 85,
    type: 'identity.pat.updated.v1',
    by: 'Sam Okafor',
    subject: 'pats/pat_2f7d13',
    entity: { id: 'pat_2f7d13', name: 'local-cli', scopes: ['agents:read', 'agents:write'] },
    changed: ['scopes'],
    version: 3,
  },
  {
    ago: 100,
    type: 'identity.pat.revoked.v1',
    by: 'Dev Operator',
    subject: 'pats/pat_0a9e44',
    entity: { id: 'pat_0a9e44', name: 'old-laptop', revoked: true },
    changed: ['revoked'],
    version: 2,
  },
  {
    ago: 120,
    type: 'declarative.resource.failed.v1',
    by: 'operator',
    subject: 'declared/Trigger/review-opened-prs',
    entity: {
      kind: 'Trigger',
      name: 'review-opened-prs',
      namespace: TENANT,
      error: 'name conflict with a trigger created through the API',
    },
    noTrace: true,
  },
  {
    ago: 130,
    type: 'triggers.trigger.created.v1',
    by: 'operator',
    subject: 'triggers/trg_01JF2B6W',
    entity: {
      id: 'trg_01JF2B6W',
      name: 'pr-merged-release-notes',
      kind: 'webhook',
      agent: 'release-notes',
      managed_by: 'declared',
    },
    version: 1,
    agent: 'release-notes',
  },
  {
    ago: 150,
    type: 'policy.role.created.v1',
    by: 'Dev Operator',
    subject: 'roles/rol_release-managers',
    entity: {
      id: 'rol_release-managers',
      name: 'release-managers',
      scopes: ['agents:run:release-notes', 'triggers:fire'],
    },
    version: 1,
  },
  {
    ago: 165,
    type: 'agents.agent.updated.v1',
    by: 'Sam Okafor',
    subject: 'agents/release-notes',
    entity: {
      slug: 'release-notes',
      name: 'release-notes',
      model: 'release-writer',
      environment: 'python-3-12',
      instructions_chars: 2140,
    },
    changed: ['model', 'instructions'],
    version: 11,
    agent: 'release-notes',
  },
  {
    ago: 180,
    type: 'identity.tenant.switched.v1',
    by: 'Sam Okafor',
    subject: 'sessions/ses_4e1b77',
    entity: { session_id: 'ses_4e1b77', to_tenant: TENANT, from_tenant: 'kora-tenant-staging' },
  },
  {
    ago: 200,
    type: 'policy.tenant_budget.updated.v1',
    by: 'Dev Operator',
    subject: 'tenant_budgets/kora-tenant-example',
    entity: { limit_usd: 5000, period: 'monthly', managed_by: 'api' },
    changed: ['limit_usd'],
    version: 3,
  },
  {
    ago: 240,
    type: 'triggers.source.updated.v1',
    by: 'Dev Operator',
    subject: 'sources/src_example-org-github',
    entity: {
      id: 'src_example-org-github',
      name: 'example-org-github',
      provider: 'github',
      events: ['pull_request', 'issues', 'check_run'],
    },
    changed: ['events'],
    version: 5,
  },
  {
    ago: 300,
    type: 'agents.sandbox.deleted.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.idleOut}/sandbox`,
    entity: { id: `sbx_${RUN.idleOut.slice(0, 8)}`, environment: 'triage-node-22' },
    run: RUN.idleOut,
    agent: 'triage',
  },
  {
    ago: 301,
    type: 'agents.run.completed.v1',
    by: 'triage',
    subject: `runs/${RUN.idleOut}`,
    entity: { id: RUN.idleOut, state: 'completed', reason: 'idle_timeout' },
    changed: ['state'],
    version: 6,
    run: RUN.idleOut,
    agent: 'triage',
    triggers: ['trg_01JC7Q2M'],
  },
  {
    ago: 360,
    type: 'policy.grant.deleted.v1',
    by: 'Priya Raman',
    subject: 'grants/grt_19b2e0d7',
    entity: {
      id: 'grt_19b2e0d7',
      name: 'legacy-ci-read',
      principal: 'pat:ci-legacy',
      scopes: ['agents:read'],
    },
    version: 7,
  },
];

function envelope(spec: Spec, n: number, at: number, ids: string[]): KoraEvent {
  const principal = who(spec.by);
  const time = new Date(at).toISOString();
  const domain = spec.type.split('.')[0] ?? '';
  return {
    specversion: '1.0',
    id: uuid7(at, n),
    source: spec.source ?? SERVICE_OF[domain] ?? 'kora-agents',
    type: spec.type,
    subject: spec.subject,
    time,
    datacontenttype: 'application/json',
    koratenant: TENANT,
    koraprincipal: principal.subject,
    koraprincipaltype: principal.type,
    koraprincipalname: principal.name,
    ...(spec.delegator !== undefined && { koradelegator: spec.delegator }),
    ...(spec.run !== undefined && { korarun: spec.run }),
    ...(spec.agent !== undefined && { koraagent: spec.agent }),
    ...(spec.causedBy !== undefined && { koracausedby: ids[spec.causedBy] }),
    korachaindepth: spec.depth ?? 0,
    ...(spec.triggers !== undefined && { koratriggers: spec.triggers }),
    ...(spec.noTrace !== true && { traceparent: traceparent(n) }),
    data: {
      entity: spec.entity,
      ...(spec.changed !== undefined && { changed: spec.changed }),
      entity_version: spec.version ?? 1,
      occurred_at: new Date(at - 14).toISOString(),
    },
  };
}

/** The tenant's history, newest first, timed back from `now`. */
export function buildEvents(now: number = Date.now()): KoraEvent[] {
  const ids = SPECS.map((s, n) => uuid7(now - s.ago * 60_000, n));
  return SPECS.map((s, n) => envelope(s, n, now - s.ago * 60_000, ids));
}

/** The chart's trace URL template, as the server passes it to the page. */
export const TRACE_URL_TEMPLATE = 'https://grafana.example.com/explore?traceId={trace_id}';

/** The fixture as the read API's page: records with a seq, newest first. */
export function storyPage(events: KoraEvent[], nextCursor: string | null = null): EventPage {
  return {
    events: events.map((event, i) => ({
      seq: events.length - i,
      recorded_at: event.time,
      event,
    })),
    next_cursor: nextCursor,
  };
}
